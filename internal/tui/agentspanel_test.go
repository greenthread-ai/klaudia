package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// Foreground sub-agents running side by side each get a live row: what it is
// doing and what it has cost. Their steps stay out of the scrollback, where
// they used to interleave; what a run came to (an indented outcome line) is
// still printed, and a finished agent leaves the panel.
func TestForegroundAgentsPanel(t *testing.T) {
	m := newTestModel()
	m.resize(120, 30)
	start := func(id, kind, desc string) {
		m.renderEvent(agent.Event{Type: "tool_use", ToolName: "Agent", ToolUseID: id,
			Input: map[string]any{"subagent_type": kind, "description": desc, "prompt": "p"}})
	}
	start("a", "Explore", "survey the MCP code")
	start("b", "general-purpose", "run the tests")
	m.renderEvent(agent.Event{Type: "tool_progress", ToolName: "Agent", ToolUseID: "a", Text: "Read mcp.go", Tokens: 12300})
	m.renderEvent(agent.Event{Type: "tool_progress", ToolName: "Agent", ToolUseID: "b", Tokens: 8100})

	panel := stripANSI(m.agentsPanel())
	for _, want := range []string{"⏺ main", "◯ Explore  Read mcp.go", "12.3k tokens", "◯ general-purpose  run the tests", "8.1k tokens"} {
		if !strings.Contains(panel, want) {
			t.Errorf("panel lacks %q:\n%s", want, panel)
		}
	}
	if strings.Contains(stripANSI(m.transcript.String()), "Read mcp.go") {
		t.Error("a sub-agent's step went to the scrollback as well as the panel")
	}
	m.renderEvent(agent.Event{Type: "tool_progress", ToolName: "Agent", ToolUseID: "a", Text: "  ↳ applied 2 files"})
	if !strings.Contains(stripANSI(m.transcript.String()), "applied 2 files") {
		t.Error("an outcome line should still be printed")
	}
	m.renderEvent(agent.Event{Type: "tool_result", ToolName: "Agent", ToolUseID: "a", Content: "done"})
	if panel := stripANSI(m.agentsPanel()); strings.Contains(panel, "Explore") || !strings.Contains(panel, "general-purpose") {
		t.Errorf("finished agent still listed, or the other gone:\n%s", panel)
	}
	m.renderEvent(agent.Event{Type: "tool_use", ToolName: "Agent", ToolUseID: "c",
		Input: map[string]any{"subagent_type": "Explore", "run_in_background": true}})
	if strings.Count(stripANSI(m.agentsPanel()), "◯") != 1 {
		t.Error("a background Agent call was listed as a foreground one")
	}
}

// Every panel line fits the terminal: a row that reached the edge wrapped,
// putting its time and tokens on a line of their own.
func TestAgentsPanelFitsTheWidth(t *testing.T) {
	for _, w := range []int{40, 80, 100, 160} {
		m := newTestModel()
		m.resize(w, 30)
		m.renderEvent(agent.Event{Type: "tool_use", ToolName: "Agent", ToolUseID: "a",
			Input: map[string]any{"subagent_type": "general-purpose", "description": strings.Repeat("a long description ", 10)}})
		m.renderEvent(agent.Event{Type: "tool_progress", ToolName: "Agent", ToolUseID: "a", Tokens: 125300})
		for _, l := range strings.Split(m.agentsPanel(), "\n") {
			if got := len([]rune(stripANSI(l))); got >= w {
				t.Errorf("width %d: line is %d wide: %q", w, got, stripANSI(l))
			}
		}
	}
}
