package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/greenthread-ai/klaudia/internal/plan"
)

// planMaxRows caps the expanded panel. The panel lives in the live region,
// which clampBottom keeps shorter than the terminal by trimming from the top —
// an unbounded list on a 24-row terminal would push the streaming preview off
// and then start eating its own header.
const planMaxRows = 8

// planVisible reports whether the panel has anything to show.
//
// Hidden once every step is done, as Claude Code does: a finished list is
// history, and the "plan updated · 7/7" line in scrollback already records it.
// Kept while idle with steps remaining, which is the opposite of Codex's
// behaviour and is what its users filed as a bug (openai/codex#18920) — the
// list is most useful exactly when the turn has ended and the user is deciding
// what to say next.
func planVisible(snap plan.Snapshot) bool {
	return len(snap.Items) > 0 && snap.Remaining() > 0
}

// renderPlan draws the panel at width, or "" when it is hidden.
//
// Every line is truncated rather than wrapped. fitLiveRegion would wrap an
// over-long line safely, but a step that wraps turns a fixed-height panel into
// one whose height depends on the prose, and the row budget above is the
// whole reason the panel cannot crowd out the input.
func renderPlan(snap plan.Snapshot, collapsed bool, width int) string {
	return renderWorkList(snap, "Plan", "", collapsed, width)
}

// renderWorkList is renderPlan for any step list: title names whose list it is
// ("Plan", "Goal") and suffix follows the count (" · iteration 2/6"). One panel
// draws both, so a /goal run shows its checklist where a plan shows its steps
// instead of leaving a stale plan up while it works.
func renderWorkList(snap plan.Snapshot, title, suffix string, collapsed bool, width int) string {
	if !planVisible(snap) {
		return ""
	}
	limit := width - 4 // caption indent plus fitLiveRegion's last-column rule
	if limit < 20 {
		limit = 20
	}
	fit := func(s string) string { return ansi.Truncate(s, limit, "…") }
	count := fmt.Sprintf("%s %d/%d%s", title, snap.Completed(), len(snap.Items), suffix)

	if collapsed {
		line := "▸ " + count
		if a, ok := snap.Active(); ok {
			line += " · " + activeLabel(a)
		}
		return caption(bannerStyle.Render(fit(line)) + hintStyle.Render("  (ctrl+t)"))
	}

	lines := []string{caption(bannerStyle.Render(fit("▾ "+count)) + hintStyle.Render("  (ctrl+t to collapse)"))}
	item := func(it plan.Item) string {
		switch it.Status {
		case plan.StatusCompleted:
			return caption(toolStyle.Render(fit("  ✓ " + it.Content)))
		case plan.StatusInProgress:
			return caption(askStyle.Render(fit("  ● " + activeLabel(it))))
		default:
			return caption(fit("  ○ " + it.Content))
		}
	}

	// Short enough to show whole: keep the finished steps in place, so the
	// list reads in the order it was written.
	if len(snap.Items) <= planMaxRows-1 {
		for _, it := range snap.Items {
			lines = append(lines, item(it))
		}
		return strings.Join(lines, "\n")
	}

	// Too long: fold the finished steps into one line and show what is left,
	// in order, as far as the budget goes. What is left is what the user is
	// watching for; what is done is a count.
	budget := planMaxRows - 1
	if done := snap.Completed(); done > 0 {
		lines = append(lines, caption(toolStyle.Render(fit(fmt.Sprintf("  ✓ %d done", done)))))
		budget--
	}
	var open []plan.Item
	for _, it := range snap.Items {
		if !it.Done() {
			open = append(open, it)
		}
	}
	shown := open
	if len(open) > budget {
		shown = open[:budget-1] // one row for the "+k more" line
	}
	for _, it := range shown {
		lines = append(lines, item(it))
	}
	if rest := len(open) - len(shown); rest > 0 {
		lines = append(lines, caption(hintStyle.Render(fmt.Sprintf("  +%d more", rest))))
	}
	return strings.Join(lines, "\n")
}

// activeLabel is how the step being worked on reads: "Running the tests"
// rather than "Run the tests". The model supplies both forms; an empty
// ActiveForm falls back to the imperative rather than to a blank line.
func activeLabel(it plan.Item) string {
	if s := strings.TrimSpace(it.ActiveForm); s != "" {
		return s
	}
	return it.Content
}

// todoResultLine replaces TodoWrite's result in scrollback with a one-line
// count.
//
// The full checklist used to be reprinted after every update — the transcript
// of a ten-step plan worked through carried ten copies of it, each differing in
// one box. The panel now shows the list live; scrollback only needs to say
// that it changed and how far along it was. Counted from the tool's own output
// rather than the panel's snapshot, because the plan event for this change
// arrives after the tool_result does.
func todoResultLine(content string) string {
	total, done := 0, 0
	// Anything that is not the header or a box is kept: TodoWrite appends a
	// note when the plan file could not be written, and folding the list must
	// not fold that away with it.
	var rest []string
	for _, ln := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "[x]"):
			total++
			done++
		case strings.HasPrefix(trimmed, "[ ]"), strings.HasPrefix(trimmed, "[~]"):
			total++
		case trimmed == "" || trimmed == "Todos updated:":
		default:
			rest = append(rest, ln)
		}
	}
	if total == 0 {
		return content
	}
	return strings.Join(append([]string{fmt.Sprintf("plan updated · %d/%d done", done, total)}, rest...), "\n")
}

// exitPlanResultLine folds ExitPlanMode's result to what the user needs. The
// result is written for the model — "record the plan's steps with TodoWrite
// now…", six lines of it — and the user has just answered the question it
// reports, so all that is news to them is where the plan was saved. Anything
// that is not an approval (a refusal, a save error) is shown as written.
func exitPlanResultLine(content string) string {
	const savedAt = "The approved plan is saved at "
	if !strings.HasPrefix(content, "Plan approved") {
		return content
	}
	line := "plan approved"
	if i := strings.Index(content, savedAt); i >= 0 {
		path := content[i+len(savedAt):]
		if j := strings.IndexAny(path, ", \n"); j >= 0 {
			path = path[:j]
		}
		line += " · saved to " + path
	}
	if i := strings.Index(content, "(The plan could not be saved"); i >= 0 {
		line += " " + strings.TrimSpace(content[i:])
	}
	return line
}
