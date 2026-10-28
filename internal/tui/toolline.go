package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// One line per tool call. The call's line used to be written when it started
// ("⚙ Read main.go") and its result under it when it finished ("✓ Read · 0ms:"
// and the first eight lines of the file), so every Read cost a screenful of
// text the user had not asked to see. Now the line is written once, when the
// result is in, and says what happened: "✓ Read main.go · 35 lines". While the
// call runs, the live status line names it.
//
// Two exceptions write the header at the start, because something has to sit
// under it before the result arrives: the Agent tool, whose sub-agent's steps
// are printed beneath it as they happen, and any call that stops to ask for
// approval — the user should see what is being asked about, and the diff,
// before the card.

// pendingTool is a call whose line has not been written yet, or has been
// written early (shown) and still waits for its result.
type pendingTool struct {
	id      string
	name    string
	summary string // " main.go", already shortened for display
	input   any
	start   time.Time
	shown   bool
}

func (p *pendingTool) header() string { return p.name + p.summary }

// showsEarly lists the tools whose header is written when they start.
var showsEarly = map[string]bool{"Agent": true}

// compactTools are summarised in one line on success: their output is the
// file, or a listing, and the transcript is not the place to read it — /last
// is.
var compactTools = map[string]bool{
	"Read": true, "Glob": true, "Grep": true, "LS": true,
	"Write": true, "Edit": true, "NotebookEdit": true,
}

// startTool records a call. Its line is written now only if it has to be.
func (m *Model) startTool(ev agent.Event) {
	p := &pendingTool{
		id: ev.ToolUseID, name: ev.ToolName, input: ev.Input, start: time.Now(),
		summary: toolSummaryWith(ev.ToolName, ev.Input, m.displayPaths),
	}
	if len(m.pendingTools) == 0 {
		m.toolsSince = p.start
	}
	m.pendingTools = append(m.pendingTools, p)
	if showsEarly[ev.ToolName] {
		m.showTool(p)
	}
	m.activeToolName = ev.ToolName
	if sum := oneline(p.summary, 40); sum != "" {
		m.activeToolName += " " + sum // oneline trims the summary's leading space
	}
	m.activeToolStart = p.start
}

// showTool writes a pending call's header (and an Edit's diff) ahead of its
// result.
func (m *Model) showTool(p *pendingTool) {
	if p.shown {
		return
	}
	p.shown = true
	m.appendLine(toolStyle.Render("⚙ " + p.header()))
	diff := toolDiff(p.name, p.input)
	switch p.name {
	case "Edit":
		diff = m.editPreview(p.input, 0)
	case "Write":
		f := toolFields(p.input)
		diff = m.writePreview(f["file_path"], f["content"])
	}
	if diff != "" {
		m.appendLine(diff)
	}
}

// showPendingTools writes every call still waiting, before an approval card:
// the card is about one of them, and the user should see what led to it.
func (m *Model) showPendingTools() {
	for _, p := range m.pendingTools {
		m.showTool(p)
	}
}

// toolShown reports whether a call of this tool has its line written and is
// still waiting — the call an approval card is about.
func (m *Model) toolShown(name string) bool {
	for _, p := range m.pendingTools {
		if p.shown && p.name == name {
			return true
		}
	}
	return false
}

// shownCallDetail is the approval detail when the call's line, with its hunk
// or command, is already on screen above the card: only what that line does
// not say. Repeating it printed every edit twice, the second time as an
// unnumbered replace "old" → "new".
func shownCallDetail(req agent.ApprovalRequest) string {
	switch req.ToolName {
	case "Edit", "Write", "NotebookEdit":
		return ""
	case "Bash":
		// The tool line shows the command cut to the terminal's width and as
		// written; when that could hide part of it, the approval shows it all.
		var lines []string
		if cmd := stringField(req.Input, "command"); needsFullCommand(cmd) {
			lines = append(lines, "command: "+visible(cmd))
		}
		if req.Suggestion != "" {
			lines = append(lines, "why: "+oneline(req.Suggestion, 220))
		}
		return strings.Join(lines, "\n  ")
	}
	return permissionDetail(req)
}

// takeTool removes and returns the pending call with this id, or a stand-in
// built from the event when there is none (a result with no tool_use seen).
func (m *Model) takeTool(ev agent.Event) *pendingTool {
	for i, p := range m.pendingTools {
		if p.id == ev.ToolUseID {
			m.pendingTools = append(m.pendingTools[:i], m.pendingTools[i+1:]...)
			m.closeToolStretch()
			return p
		}
	}
	return &pendingTool{id: ev.ToolUseID, name: ev.ToolName, input: ev.Input,
		summary: toolSummaryWith(ev.ToolName, ev.Input, m.displayPaths)}
}

// closeToolStretch adds the stretch of tool time that just ended, once no
// call is left in flight.
func (m *Model) closeToolStretch() {
	if len(m.pendingTools) == 0 && !m.toolsSince.IsZero() {
		m.toolTime += time.Since(m.toolsSince)
		m.toolsSince = time.Time{}
	}
}

// takeToolTime returns this turn's tool time and resets it for the next.
func (m *Model) takeToolTime() time.Duration {
	if !m.toolsSince.IsZero() {
		m.toolTime += time.Since(m.toolsSince)
		m.toolsSince = time.Time{}
	}
	d := m.toolTime
	m.toolTime = 0
	return d
}

// abandonPendingTools writes a line for each call that never reported back —
// the turn was interrupted or failed with calls in flight — so the transcript
// does not silently lose them.
func (m *Model) abandonPendingTools() {
	for _, p := range m.pendingTools {
		if p.shown {
			m.appendLine(hintStyle.Render("  ⊘ " + p.name + " · stopped"))
		} else {
			m.appendLine(hintStyle.Render("⊘ " + p.header() + " · stopped"))
		}
	}
	m.pendingTools = nil
}

// readEntry is one successful Read waiting to be written with its run.
type readEntry struct{ name, detail string }

// flushReads writes the held run of Reads. Models read in batches — the file,
// its test, go.mod — and one line per file was three lines saying one thing.
// A run of one is written as it always was; two or more become
// "✓ Read 3 files · main.go, main_test.go, go.mod". commit calls this before
// anything else reaches scrollback, so the run lands in its place.
func (m *Model) flushReads() {
	run := m.readRun
	if len(run) == 0 {
		return
	}
	m.readRun = nil
	if len(run) == 1 {
		line := "✓ Read " + run[0].name
		if run[0].detail != "" {
			line += " · " + run[0].detail
		}
		m.appendLine(toolStyle.Render(line))
		return
	}
	names := make([]string, len(run))
	for i, r := range run {
		names[i] = r.name
	}
	m.appendLine(toolStyle.Render(fmt.Sprintf("✓ Read %s · %s", countOf(len(run), "file"), shortList(names, 6))))
}

// heldReadsLine is the line the held run would be written as, for the live
// region; "" when nothing is held.
func (m *Model) heldReadsLine() string {
	switch n := len(m.readRun); {
	case n == 0:
		return ""
	case n == 1:
		return toolStyle.Render("✓ Read " + m.readRun[0].name)
	default:
		names := make([]string, n)
		for i, r := range m.readRun {
			names[i] = r.name
		}
		return toolStyle.Render(fmt.Sprintf("✓ Read %s · %s", countOf(n, "file"), shortList(names, 6)))
	}
}

// renderToolResult writes a finished call: one line, plus whatever below it
// is worth reading — an Edit's diff, a command's output, an error.
func (m *Model) renderToolResult(ev agent.Event, p *pendingTool, seq int) {
	s := m.displayPaths(strings.TrimSpace(ev.Content))
	if s == "" {
		s = "completed"
	}

	// A header already written stands; the result goes under it, indented,
	// without repeating the summary. Otherwise this line is the header.
	head := p.header()
	indent := ""
	if p.shown {
		head, indent = p.name, "  "
	}
	if !p.start.IsZero() {
		// Under a second is noise on every line; above it, it is the reason a
		// turn was slow.
		if d := time.Since(p.start); d >= time.Second {
			head += " · " + fmtDuration(d)
		}
	}

	switch {
	case ev.HostBlocked:
		// Not a failure. The call was not allowed and the model will take
		// another route, which is the intended behaviour for the common case —
		// an incidental 2>/dev/null, a scratch file in /tmp. Drawing that in
		// red with a paragraph of policy made Klaudia look broken while it was
		// working exactly as designed.
		m.appendLine(hintStyle.Render(indent + "⊘ " + head + ": " + hostBlockedLine(s)))
		return
	case ev.IsError:
		m.appendLine(errStyle.Render(indent + "✗ " + head))
		m.appendPreview(s, indent, errStyle, seq)
		return
	case ev.ToolName == "TodoWrite":
		m.appendLine(toolStyle.Render(indent + "✓ " + head + ": " + todoResultLine(s)))
		return
	case ev.ToolName == "Bash" && dataString(ev.Data, "job") != "":
		// A background start: the result text is how-to for the model
		// (BashOutput, RestartJob, KillShell), three lines the user did not
		// need. One line says what happened.
		verb := "started as "
		if ev.Data["duplicate"] == true {
			verb = "already running as "
		}
		m.appendLine(toolStyle.Render(indent + "✓ " + head + " · " + verb + dataString(ev.Data, "job")))
		return
	case ev.ToolName == "ExitPlanMode":
		m.appendLine(toolStyle.Render(indent + "✓ " + head + ": " + exitPlanResultLine(s)))
		return
	case compactTools[ev.ToolName]:
		d := compactDetail(ev.ToolName, s, p.input)
		if ev.ToolName == "Read" && !p.shown && head == p.header() {
			// Held, so a run of reads becomes one line (flushReads).
			m.readRun = append(m.readRun, readEntry{name: strings.TrimSpace(p.summary), detail: d})
			return
		}
		if d != "" {
			head += " · " + d
		}
		m.appendLine(toolStyle.Render(indent + "✓ " + head))
		if !p.shown {
			if diff := m.editDiff(ev.ToolName, p.input, dataInt(ev.Data, "line")); diff != "" {
				m.appendLine(diff)
			}
		}
		return
	}
	m.appendLine(toolStyle.Render(indent + "✓ " + head))
	// Syntax-highlight fenced code in results, else truncated plain text. Skip
	// the Markdown path for cat -n line-numbered output: glamour treats it as
	// prose and reflows every line into one run-on block.
	if strings.Contains(s, "```") && len(s) <= 4000 && !looksLineNumbered(s) {
		m.appendLine(m.markdown(s))
		return
	}
	m.appendPreview(s, indent, toolStyle, seq)
}

// appendPreview writes a result's text under its line, clipped, naming the
// /last number that shows the rest.
func (m *Model) appendPreview(s, indent string, style interface{ Render(...string) string }, seq int) {
	if s == "completed" {
		return
	}
	pad := indent + "    "
	clipped, dropped := clipPreview(s, maxPreviewLines, maxPreviewRunes)
	if dropped > 0 {
		clipped += fmt.Sprintf("\n…  %s", hintStyle.Render(
			fmt.Sprintf("(%d more lines · /last %d for full output)", dropped, seq)))
	}
	m.appendLine(style.Render(pad + strings.ReplaceAll(clipped, "\n", "\n"+pad)))
}

// editDiff is the change preview written under a finished edit. Write's is
// left out: the line already says how many lines it wrote, and its first three
// lines told nobody anything.
func (m *Model) editDiff(name string, input any, line int) string {
	switch name {
	case "Write":
		return ""
	case "Edit":
		return m.editPreview(input, line)
	}
	return toolDiff(name, input)
}

// editPreview is an Edit's numbered hunk; line is where the tool reported the
// change starts, or 0.
func (m *Model) editPreview(input any, line int) string {
	f := toolFields(input)
	return m.editHunk(f["file_path"], f["old_string"], f["new_string"], line)
}

// dataString reads a string a tool reported in Result.Data.
func dataString(data map[string]any, key string) string {
	s, _ := data[key].(string)
	return s
}

// dataInt reads an integer a tool reported in Result.Data: an int in
// process, a float64 once it has been through JSON.
func dataInt(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}

// readLineRe matches a line of Read's numbered output.
var readLineRe = regexp.MustCompile(`(?m)^\s*\d+\t`)

// compactDetail is what a compact tool's line says about its result.
func compactDetail(name, content string, input any) string {
	lines := 0
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) != "" {
			lines++
		}
	}
	switch name {
	case "Read":
		// Every line of the file carries its number, blank ones included;
		// notes the tool appends after the file do not.
		if n := len(readLineRe.FindAllStringIndex(content, -1)); n > 0 {
			return countOf(n, "line")
		}
		return countOf(lines, "line")
	case "Write":
		return countOf(strings.Count(strings.TrimRight(toolFields(input)["content"], "\n"), "\n")+1, "line")
	case "Glob", "LS":
		if lines == 0 || strings.HasPrefix(content, "No files") {
			return "none found"
		}
		return countOf(lines, "file")
	case "Grep":
		if lines == 0 || strings.HasPrefix(content, "No ") {
			return "no matches"
		}
		return countOf(lines, "result")
	}
	return ""
}
