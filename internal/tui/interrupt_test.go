package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"
)

// isQuit reports whether cmd resolves to tea.Quit — directly, or as the last
// step of a tea.Sequence (quit prints a line first). Bubble Tea's sequence
// message is unexported, so it is recognised by shape: a slice of commands.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		return true
	}
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Len() > 0 {
		if last, ok := v.Index(v.Len() - 1).Interface().(tea.Cmd); ok {
			return isQuit(last)
		}
	}
	return false
}

func ctrlC(m *Model) (*Model, tea.Cmd) {
	model, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	return model.(*Model), cmd
}

func TestCtrlCInterruptsTurnBeforeQuitting(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.setState(stateRunning)
	cancelled := false
	m.turnCancel = func() { cancelled = true }

	m, cmd := ctrlC(m)
	if isQuit(cmd) {
		t.Fatal("first Ctrl+C mid-turn quit the session instead of interrupting")
	}
	if !cancelled {
		t.Fatal("first Ctrl+C mid-turn did not cancel the turn")
	}
	if !m.cancelling {
		t.Error("expected cancelling state after interrupt")
	}
	if !m.quitArmed {
		t.Error("interrupting should arm quit so a wedged turn can still be escaped")
	}

	// Second press escalates to a force quit, as the hint promises.
	if _, cmd = ctrlC(m); !isQuit(cmd) {
		t.Fatal("second Ctrl+C should force quit")
	}
}

func TestCtrlCHaltsGoalLoop(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.setState(stateRunning)
	m.turnCancel = func() {}
	m.loopRemaining, m.loopTotal, m.loopWrapUp = 3, 5, true

	m, _ = ctrlC(m)
	if m.loopRemaining != 0 || m.loopWrapUp {
		t.Fatalf("Ctrl+C should halt the goal loop, got remaining=%d wrapUp=%v", m.loopRemaining, m.loopWrapUp)
	}
}

func TestCtrlCClearsDraftBeforeQuitting(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.input.SetValue("a carefully written prompt")

	m, cmd := ctrlC(m)
	if isQuit(cmd) {
		t.Fatal("Ctrl+C with a draft quit instead of clearing the line")
	}
	if m.input.Value() != "" {
		t.Fatalf("draft not cleared, still %q", m.input.Value())
	}
	if m.quitArmed {
		t.Error("clearing a draft should not arm quit")
	}
}

func TestCtrlCCancelsLocalPrompts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*Model)
	}{
		{"confirm", func(m *Model) {
			m.confirmAction = func() string { return "ran" }
			m.setState(stateAwaitingConfirm)
		}},
		{"choice", func(m *Model) {
			m.startChoice("Pick one", []choiceItem{{label: "a", apply: func() string { return "a" }}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			m.resize(80, 24)
			tc.setup(m)

			m, cmd := ctrlC(m)
			if isQuit(cmd) {
				t.Fatal("Ctrl+C at a local prompt quit instead of cancelling it")
			}
			if m.state != stateIdle {
				t.Fatalf("state = %v, want idle", m.state)
			}
			if m.confirmAction != nil || m.choiceItems != nil {
				t.Error("pending prompt not cleared")
			}
		})
	}
}

func TestCtrlCTwicePlainQuits(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)

	m, cmd := ctrlC(m)
	if isQuit(cmd) {
		t.Fatal("a single Ctrl+C on an empty idle line should not quit")
	}
	if !m.quitArmed {
		t.Fatal("expected quit to be armed")
	}
	if bottom := m.bottomView(); !strings.Contains(bottom, "again to quit") {
		t.Errorf("armed state not surfaced to the user:\n%s", bottom)
	}
	if _, cmd = ctrlC(m); !isQuit(cmd) {
		t.Fatal("second consecutive Ctrl+C should quit")
	}
}

func TestAnyOtherKeyDisarmsQuit(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)

	m, _ = ctrlC(m)
	if !m.quitArmed {
		t.Fatal("expected quit to be armed")
	}
	model, _ := m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = model.(*Model)
	if m.quitArmed {
		t.Fatal("typing should disarm the pending quit")
	}
	if _, cmd := ctrlC(m); isQuit(cmd) {
		t.Fatal("Ctrl+C after a disarming keystroke should re-arm, not quit")
	}
}

// Ctrl+U and Ctrl+D are readline's kill-line and delete-forward. They used to be
// bound to viewport paging, which meant they never reached the input at all.
func TestCtrlUReachesTheInput(t *testing.T) {
	m := newScrollableTestModel(t)
	m.setState(stateIdle)
	m.input.SetValue("some text I want to clear")
	m.input.CursorEnd()

	model, _ := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = model.(*Model)
	if m.input.Value() != "" {
		t.Fatalf("Ctrl+U did not kill the line, input still %q", m.input.Value())
	}
}

func TestCtrlDDeletesForward(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.input.SetValue("abc")
	m.input.SetCursor(0)

	model, _ := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	m = model.(*Model)
	if got := m.input.Value(); got != "bc" {
		t.Fatalf("Ctrl+D should delete forward, got %q want %q", got, "bc")
	}
}

// Quitting says how to come back, then draws nothing, so the input box does not
// stay behind in scrollback.
func TestQuitLeavesAResumeLineAndAnEmptyFrame(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	m.sess.SessionID = "abc-123"
	m.history = make([]anthropic.BetaMessageParam, 1)
	_, cmd := m.handleSlash("/quit")
	if !isQuit(cmd) {
		t.Fatal("/quit should quit")
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "klaudia -r abc-123") {
		t.Errorf("no resume line:\n%s", out)
	}
	if v := m.View(); v != "" {
		t.Errorf("final frame should be empty, got %q", v)
	}
}
