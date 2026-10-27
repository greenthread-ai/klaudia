package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The box should behave the same way regardless of what the agent is doing
// behind it. Every bug in this area has been the same shape: an affordance
// gated on one state while the box that carries it exists in three, so a
// feature silently stopped working "in thinking mode".
//
// This is the general guard. It drives each affordance through the real key
// path in every state that has a box, so adding an affordance that only works
// while idle fails here rather than in a live session.
func TestTheBoxBehavesTheSameInEveryStateThatHasOne(t *testing.T) {
	editable := []struct {
		name  string
		state uiState
	}{
		{"idle", stateIdle},
		{"running", stateRunning},
		{"other-answer", stateAnsweringOther},
	}

	// Each affordance: set the box up, exercise it, say whether it worked.
	affordances := []struct {
		name string
		run  func(t *testing.T, m *Model, dir string) bool
	}{
		{"typing reaches the box", func(t *testing.T, m *Model, dir string) bool {
			typeInto(m, "hello")
			return m.input.Value() == "hello"
		}},
		{"@ opens the menu", func(t *testing.T, m *Model, dir string) bool {
			typeInto(m, "see @alp")
			return m.pathMenuView() != "" && strings.Contains(m.bottomView(), "alpha.go")
		}},
		{"tab completes a reference", func(t *testing.T, m *Model, dir string) bool {
			typeInto(m, "see @alp")
			m.onKey(tea.KeyPressMsg{Code: tea.KeyTab})
			return strings.Contains(m.input.Value(), "alpha.go")
		}},
		{"arrows pick from the menu", func(t *testing.T, m *Model, dir string) bool {
			typeInto(m, "see @alp")
			m.onKey(tea.KeyPressMsg{Code: tea.KeyDown})
			return strings.Contains(m.input.Value(), "alp")
		}},
		{"history recalls earlier input", func(t *testing.T, m *Model, dir string) bool {
			m.pushHistory("an earlier line")
			m.onKey(tea.KeyPressMsg{Code: tea.KeyUp})
			return m.input.Value() == "an earlier line"
		}},
		{"paste lands in the box", func(t *testing.T, m *Model, dir string) bool {
			paste(m, "pasted text")
			return strings.Contains(m.input.Value(), "pasted")
		}},
		{"the box grows with its content", func(t *testing.T, m *Model, dir string) bool {
			m.input.SetValue("one\ntwo\nthree\nfour")
			return m.inputHeight() > 1
		}},
	}

	for _, a := range affordances {
		t.Run(a.name, func(t *testing.T) {
			for _, st := range editable {
				t.Run(st.name, func(t *testing.T) {
					dir := t.TempDir()
					mustWrite(t, filepath.Join(dir, "alpha.go"))
					m := menuModel(t, dir)
					m.setState(st.state)
					if !a.run(t, m, dir) {
						t.Errorf("%q does not work in %s (input %q)", a.name, st.name, m.input.Value())
					}
				})
			}
		})
	}
}

// The "$" marker exists so that what Enter will do is visible before Enter is
// pressed (syncPromptMarker). It was shown in every state with a box, but "!"
// only runs on an idle line — so while the model worked it promised a local
// shell command and then sent the text to the model instead.
func TestBangMarkerOnlyAppearsWhereBangRuns(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state uiState
		runs  bool
	}{
		{"idle", stateIdle, true},
		{"running", stateRunning, false},
		{"other-answer", stateAnsweringOther, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := menuModel(t, t.TempDir())
			m.setState(tt.state)
			typeInto(m, "!ls")

			if shown := strings.Contains(m.promptBox(), "$"); shown != tt.runs {
				t.Errorf("the $ marker is shown = %v, want %v", shown, tt.runs)
			}
			if m.bangRunsHere() != tt.runs {
				t.Errorf("bangRunsHere = %v, want %v", m.bangRunsHere(), tt.runs)
			}
		})
	}
}

// A "!" line while the model works is refused, not queued. Sending it to the
// model would have it run something like the command through Bash — sandboxed,
// gated, and not the thing that was asked for. The text stays put so that
// interrupting and pressing Enter again does what was meant.
func TestBangIsRefusedNotQueuedWhileWorking(t *testing.T) {
	m := menuModel(t, t.TempDir())
	m.setState(stateRunning)
	typeInto(m, "!git status")
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.steer.pending() {
		t.Error("the ! line was queued as a message to the model")
	}
	if m.input.Value() != "!git status" {
		t.Errorf("the command was taken out of the box: %q", m.input.Value())
	}
	if !strings.Contains(drain(t, &m.out), "esc to interrupt") {
		t.Error("nothing told the user why it did not run")
	}
}

// Up means "give me back what I typed". While the model works the queued
// follow-up is the most recent thing typed, so it comes back first — but once
// it has, Up keeps walking back into the prompt history rather than going dead.
func TestUpRecallsTheQueuedMessageThenTheHistory(t *testing.T) {
	m := menuModel(t, t.TempDir())
	m.pushHistory("an earlier prompt")
	m.setState(stateRunning)
	m.steer.add("queued follow-up", "queued follow-up")

	m.onKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.input.Value(); got != "queued follow-up" {
		t.Fatalf("first Up gave %q, want the queued message", got)
	}
	m.input.Reset()
	m.onKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.input.Value(); got != "an earlier prompt" {
		t.Errorf("second Up gave %q, want the history entry", got)
	}
}

// The arrows still move the cursor inside a draft. Recalling history out from
// under a half-written multi-line message would lose it.
func TestHistoryLeavesAMultiLineDraftAlone(t *testing.T) {
	for _, st := range []uiState{stateIdle, stateRunning, stateAnsweringOther} {
		m := menuModel(t, t.TempDir())
		m.setState(st)
		m.pushHistory("an earlier prompt")
		m.input.SetValue("first line\nsecond line")
		m.onKey(tea.KeyPressMsg{Code: tea.KeyUp})
		if m.input.Value() != "first line\nsecond line" {
			t.Errorf("state %v: Up ate the draft: %q", st, m.input.Value())
		}
	}
}
