package tui

import (
	"strings"
	"testing"

	"github.com/greenthread/klaudia/internal/permission"
)

func TestToolSummary(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "go test ./..."}, " go test ./..."},
		{"Edit", map[string]any{"file_path": "internal/lsp/pool.go"}, " internal/lsp/pool.go"},
		{"Grep", map[string]any{"pattern": "func New"}, " func New"},
		{"WebSearch", map[string]any{"query": "go lsp client"}, " go lsp client"},
		{"Read", map[string]any{}, ""}, // missing key → no summary
	}
	for _, c := range cases {
		if got := toolSummary(c.name, c.input); got != c.want {
			t.Errorf("%s summary = %q, want %q", c.name, got, c.want)
		}
	}
	// Agent shows subagent type + quoted description.
	got := toolSummary("Agent", map[string]any{"subagent_type": "Plan", "description": "design the change"})
	if !strings.Contains(got, "(Plan)") || !strings.Contains(got, "design the change") {
		t.Errorf("Agent summary = %q", got)
	}
	// Long input is collapsed and truncated.
	long := toolSummary("Bash", map[string]any{"command": strings.Repeat("x", 200)})
	if len([]rune(long)) > maxSummary+2 || !strings.HasSuffix(long, "…") {
		t.Errorf("long summary not truncated: %d runes", len([]rune(long)))
	}
}

func TestToolDiff(t *testing.T) {
	edit := toolDiff("Edit", map[string]any{"old_string": "a := 1", "new_string": "a := 2"})
	if !strings.Contains(edit, "- ") || !strings.Contains(edit, "+ ") {
		t.Errorf("edit diff missing -/+ hunk: %q", edit)
	}
	write := toolDiff("Write", map[string]any{"content": "line1\nline2\nline3\nline4\nline5"})
	if !strings.Contains(write, "+ ") || !strings.Contains(write, "(5 lines)") {
		t.Errorf("write summary wrong: %q", write)
	}
	if toolDiff("Read", map[string]any{"file_path": "x"}) != "" {
		t.Error("non-mutating tool should have no diff")
	}
}

func TestShortMode(t *testing.T) {
	cases := map[permission.Mode]string{
		permission.ModeDefault:           "ask",
		permission.ModeAcceptEdits:       "auto-edit",
		permission.ModePlan:              "plan",
		permission.ModeDontAsk:           "deny",
		permission.ModeBypassPermissions: "bypass",
	}
	for mode, want := range cases {
		if got := shortMode(mode); got != want {
			t.Errorf("shortMode(%s) = %q, want %q", mode, got, want)
		}
	}
}

func TestStatusLine(t *testing.T) {
	m := &Model{sess: &Session{Model: "openai/gpt-5.5", PermissionMode: "plan"}, statTurns: 3, statIn: 1000, statOut: 240}
	got := stripANSI(m.statusLine())
	for _, want := range []string{"openai/gpt-5.5", "plan", "3 turns", "1.2k tokens"} {
		if !strings.Contains(got, want) {
			t.Errorf("statusLine missing %q in %q", want, got)
		}
	}
}
