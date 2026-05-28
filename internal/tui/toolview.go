package tui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

var (
	diffAddStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	diffDelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
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

// statusLine renders the persistent context bar: model · mode · turns · tokens.
func (m *Model) statusLine() string {
	model := "(default)"
	if m.sess != nil {
		if dm := m.sess.displayModel(); dm != "" {
			model = dm
		}
	}
	toks := m.statIn + m.statOut
	line := fmt.Sprintf("%s · %s · %d turns · %s tokens",
		model, shortMode(m.currentMode()), m.statTurns, humanTokens(toks))
	if m.loopRemaining > 0 {
		// While the /goal loop runs, show which iteration we're on.
		line += fmt.Sprintf(" · goal %d/%d", m.loopTotal-m.loopRemaining+1, m.loopTotal)
	} else if m.loopWrapUp {
		line += " · goal summary"
	} else if m.goalSetting {
		line += " · goal-setting"
	}
	return hintStyle.Render(line)
}
