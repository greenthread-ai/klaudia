package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQueueWhileRunning(t *testing.T) {
	m := newTestModel()
	m.state = stateRunning
	cancelled := false
	m.turnCancel = func() { cancelled = true }

	// Type a follow-up and press Enter → it's queued, input cleared.
	m.input.SetValue("also add tests")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.queued != "also add tests" {
		t.Fatalf("queued = %q, want the typed text", m.queued)
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		t.Errorf("input should be cleared after queueing, got %q", m.input.Value())
	}

	// Enter again on an empty input → interrupt the running turn.
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !cancelled {
		t.Error("a second Enter (empty) should interrupt the turn to send the queued message")
	}

	// ↑ recalls the queued message into the input for editing (and clears the queue).
	m.input.SetValue("v2")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter}) // re-queue "v2"
	m.onKey(tea.KeyMsg{Type: tea.KeyUp})    // recall to edit
	if m.queued != "" {
		t.Errorf("queue should clear when recalled for editing, still %q", m.queued)
	}
	if m.input.Value() != "v2" {
		t.Errorf("recalled input = %q, want v2", m.input.Value())
	}
}

func TestQueueEnterWithoutTextOrQueueIsNoop(t *testing.T) {
	m := newTestModel()
	m.state = stateRunning
	// Empty input, nothing queued → Enter does nothing (no panic, no queue).
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.queued != "" {
		t.Errorf("nothing should be queued, got %q", m.queued)
	}
}
