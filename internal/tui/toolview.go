package tui

import (
	"encoding/json"
	"fmt"
	"github.com/charmbracelet/x/ansi"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

var (
	diffAddStyle = baseStyle().Foreground(lipgloss.Color("2")) // green
	diffDelStyle = baseStyle().Foreground(lipgloss.Color("1")) // red
)

const (
	maxSummary  = 80 // chars of the inline tool-input summary
	maxDiffLine = 100
	maxDiffRows = 12
)

// summaryKey is the most salient input field to echo per tool.
var summaryKey = map[string]string{
	"Bash": "command", "BashOutput": "bash_id", "KillShell": "shell_id",
	"Read": "file_path", "Write": "file_path", "Edit": "file_path", "NotebookEdit": "notebook_path",
	"Diagnostics": "file", "Definition": "file", "References": "file",
	"Glob": "pattern", "Grep": "pattern",
	"WebSearch": "query", "WebFetch": "url", "BrowserNavigate": "url",
	"Skill": "name", "ToolSearch": "query",
	"TaskCreate": "description", "TaskGet": "task_id", "TaskUpdate": "task_id",
}

// toolFields decodes a tool_use Input (any) into a flat string map.
func toolFields(input any) map[string]string {
	out := map[string]string{}
	b, err := json.Marshal(input)
	if err != nil {
		return out
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return out
	}
	for k, v := range raw {
		switch s := v.(type) {
		case string:
			out[k] = s
		case float64:
			out[k] = strconv.FormatFloat(s, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(s)
		}
	}
	return out
}

// toolSummary returns a short " <salient input>" suffix for a tool_use header,
// e.g. " go test ./..." for Bash. Empty when there's nothing useful to show.
func toolSummary(name string, input any) string {
	f := toolFields(input)
	if name == "Agent" {
		sub := f["subagent_type"]
		desc := oneline(f["description"], maxSummary)
		if sub != "" && desc != "" {
			return fmt.Sprintf("(%s) %q", sub, desc)
		}
		if sub != "" {
			return " " + sub
		}
	}
	if key, ok := summaryKey[name]; ok {
		if v := oneline(f[key], maxSummary); v != "" {
			return " " + v
		}
	}
	return ""
}

// oneline collapses whitespace/newlines and truncates to n runes.
func oneline(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > n {
		s = string([]rune(s)[:n-1]) + "…"
	}
	return s
}

// toolDiff renders a compact, indented change preview for mutating tools
// (Edit shows a -/+ hunk; Write/NotebookEdit a "+N lines" summary). Empty
// otherwise.
func toolDiff(name string, input any) string {
	f := toolFields(input)
	switch name {
	case "Edit":
		return diffHunk(f["old_string"], f["new_string"])
	case "Write":
		return addedSummary(f["content"])
	case "NotebookEdit":
		return addedSummary(f["new_source"])
	}
	return ""
}

func diffHunk(oldS, newS string) string {
	var rows []string
	add := func(prefix string, st lipgloss.Style, text string) {
		for _, ln := range strings.Split(text, "\n") {
			if len(rows) >= maxDiffRows {
				return
			}
			rows = append(rows, "  "+st.Render(prefix+oneline(ln, maxDiffLine)))
		}
	}
	if oldS != "" {
		add("- ", diffDelStyle, oldS)
	}
	if newS != "" {
		add("+ ", diffAddStyle, newS)
	}
	if len(rows) == 0 {
		return ""
	}
	if len(rows) >= maxDiffRows {
		rows = append(rows, "  "+bannerStyle.Render("…"))
	}
	return strings.Join(rows, "\n")
}

func addedSummary(content string) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	var rows []string
	for i, ln := range lines {
		if i >= 3 || len(rows) >= maxDiffRows {
			break
		}
		rows = append(rows, "  "+diffAddStyle.Render("+ "+oneline(ln, maxDiffLine)))
	}
	rows = append(rows, "  "+bannerStyle.Render(fmt.Sprintf("(%d lines)", len(lines))))
	return strings.Join(rows, "\n")
}

// shortMode is a compact permission-mode label for the status bar.
func shortMode(m permission.Mode) string {
	switch m {
	case permission.ModeAutonomous:
		// Must be listed explicitly. Falling through to the default meant an
		// autonomous session displayed "ask" — the status bar claiming Klaudia
		// would stop and check, while it was in fact working straight through.
		// Of everything on this line, the mode is the one thing that must not
		// be wrong.
		return "autonomous"
	case permission.ModeAcceptEdits:
		return "auto-edit"
	case permission.ModeBypassPermissions:
		return "bypass"
	case permission.ModePlan:
		return "plan"
	case permission.ModeDontAsk:
		return "deny"
	default:
		return "ask"
	}
}

// deviatingMode returns the mode label to show, or "" when the session is
// simply working normally.
//
// Both defaults are silent: autonomous for a current config, and ask for one
// written before the mode collapse. Each is the steady state for whoever is
// running it, and neither tells them anything they do not already assume.
func deviatingMode(m permission.Mode) string {
	switch m {
	case permission.ModeAutonomous, permission.ModeDefault:
		return ""
	}
	return shortMode(m)
}

// modeSegmentStyle picks the styling for the mode segment.
//
// Bypass is the one mode where nothing is checking anything, and in the same
// dim grey as the token count it read as ordinary chrome. Everything else on
// this line is information; that one is a warning.
func modeSegmentStyle(m permission.Mode) lipgloss.Style {
	if m == permission.ModeBypassPermissions {
		return warnStyle
	}
	return hintStyle
}

// runningJobCount is how many managed jobs are up, or 0 when jobs are not
// available in this session.
func (m *Model) runningJobCount() int {
	if m.sess == nil || m.sess.Jobs == nil {
		return 0
	}
	n := 0
	for _, j := range m.sess.Jobs.List() {
		if j.Running {
			n++
		}
	}
	return n
}

// statusLine renders the context caption under the input box.
//
// A pending "press Ctrl+C again to quit" takes over the whole line — it is a
// transient, one-keystroke-lived state and the user needs to see it without
// hunting.
//
// Otherwise the line is assembled from segments in priority order and truncated
// by dropping whole segments, not characters: on a narrow terminal "opus-5 ·
// ctx 5%" is useful where "opus-5 · ctx 5% · 0 tur" is just broken.
//
// The permission mode appears only when it deviates from the ordinary. Once
// autonomous became the default it was on the line in almost every session, and
// a segment that never changes stops being read — while still outranking the
// context percentage, which is the one number here anyone acts on. Plan and
// bypass are worth interrupting for; "working normally" is not.
func (m *Model) statusLine() string {
	if m.quitArmed {
		return askStyle.Render("Press Ctrl+C again to quit") +
			hintStyle.Render("  ·  any other key cancels")
	}

	model := "(default)"
	if m.sess != nil {
		if dm := m.sess.displayModel(); dm != "" {
			model = dm
		}
	}
	segments := []string{hintStyle.Render(model)}
	if mode := deviatingMode(m.currentMode()); mode != "" {
		segments = append(segments, modeSegmentStyle(m.currentMode()).Render(mode))
	}
	// Something running is a fact you forget until the next start collides with
	// it. Shown only when there is one, so it costs nothing the rest of the time.
	if n := m.runningJobCount(); n > 0 {
		noun := "jobs"
		if n == 1 {
			noun = "job"
		}
		segments = append(segments, hintStyle.Render(fmt.Sprintf("%d %s", n, noun)))
	}

	// Goal state outranks the counters: it says the session is doing something
	// unattended, which is the thing you would most want to notice.
	switch {
	case m.loopRemaining > 0:
		segments = append(segments, hintStyle.Render(fmt.Sprintf("goal %d/%d", m.loopTotal-m.loopRemaining+1, m.loopTotal)))
	case m.loopWrapUp:
		segments = append(segments, hintStyle.Render("goal summary"))
	case m.goalSetting:
		segments = append(segments, hintStyle.Render("goal-setting"))
	}
	// Context pressure is actionable (it tells you when to /compact); raw turn
	// and token counts are just information.
	if m.sess != nil && m.sess.ContextWindow > 0 && m.residentTokens > 0 {
		pct := float64(m.residentTokens) / float64(m.sess.ContextWindow) * 100
		segments = append(segments, hintStyle.Render(fmt.Sprintf("ctx %.0f%%", pct)))
	}
	segments = append(segments,
		hintStyle.Render(fmt.Sprintf("%d turns", m.statTurns)),
		hintStyle.Render(fmt.Sprintf("%s tokens", humanTokens(m.statIn+m.statOut))))

	// width-3: two columns for the caption indent, and one for the reserved
	// last column the live region never writes to (see promptBox).
	// Segments carry their own styling now, so the line is assembled rather
	// than wrapped: wrapping a pre-styled run in hintStyle would reset the
	// colour the bypass warning depends on.
	return fitSegments(segments, m.width-3, hintStyle.Render(" · "))
}

// statusLineAt renders the caption as if the terminal were width cells wide.
// Exists so the segment-dropping can be tested without a full resize.
func (m *Model) statusLineAt(width int) string {
	saved := m.width
	m.width = width
	defer func() { m.width = saved }()
	return m.statusLine()
}

// fitSegments joins segments with " · ", dropping from the low-priority end
// until the result fits. The first segment is always kept, even when it alone
// overflows — a clipped model name still identifies the model. A width of zero
// means the terminal size isn't known yet (no WindowSizeMsg has arrived), which
// is not the same as "no room": keep everything rather than collapsing to one
// segment on the strength of a value we haven't been told.
func fitSegments(segments []string, width int, sep string) string {
	if width <= 0 {
		return strings.Join(segments, sep)
	}
	for n := len(segments); n > 1; n-- {
		// ansi.StringWidth, not len([]rune): a segment may carry its own colour
		// (bypass does), and counting escape bytes as columns would drop
		// segments that fit. It is also simply more correct for wide runes.
		if line := strings.Join(segments[:n], sep); ansi.StringWidth(line) <= width {
			return line
		}
	}
	return segments[0]
}
