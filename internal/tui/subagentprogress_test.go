package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/agent"
)

// A sub-agent's relayed tool calls belong in scrollback, indented under the tool
// they came from. They are a record of work that happened — not transient state
// — and they are the only sign the user gets that a long sub-agent is moving.
func TestToolProgressIsRenderedNestedInScrollback(t *testing.T) {
	m := newTestModel()
	m.renderEvent(agent.Event{Type: "tool_use", ToolName: "Agent",
		Input: map[string]any{"description": "research pricing"}})
	m.renderEvent(agent.Event{Type: "tool_progress", ToolName: "Agent",
		ToolUseID: "tu1", Text: "Read pricing.go"})

	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "Read pricing.go") {
		t.Fatalf("sub-agent progress never reached scrollback:\n%s", out)
	}
	// Indented, so it reads as one level under the Agent call rather than as a
	// tool the main loop ran itself.
	var found bool
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Read pricing.go") {
			found = true
			if !strings.HasPrefix(line, " ") {
				t.Errorf("progress line is not indented under its parent: %q", line)
			}
		}
	}
	if !found {
		t.Error("progress line not found")
	}
}

// The activity clock must advance, or the live region starts claiming Klaudia
// has gone quiet while a sub-agent is visibly working.
func TestToolProgressCountsAsActivity(t *testing.T) {
	m := newTestModel()
	before := m.lastEventAt
	m.renderEvent(agent.Event{Type: "tool_progress", ToolName: "Agent", Text: "Grep foo"})
	if !m.lastEventAt.After(before) {
		t.Error("progress did not bump the activity clock; the stuck-state hint would fire")
	}
}
