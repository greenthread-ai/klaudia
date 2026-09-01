package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// askTwo puts a two-option question to the model under test and returns the
// reply channel the tool would be blocking on.
func askTwo(t *testing.T) (*Model, chan string) {
	t.Helper()
	m := newTestModel()
	reply := make(chan string, 1)
	m.update(askMsg{
		question: "Which problem are you actually trying to solve?",
		options: []tools.AskOption{
			{Label: "Explain the constraint"},
			{Label: "Version skew"},
		},
		reply: reply,
	})
	if m.state != stateAwaitingAnswer {
		t.Fatalf("state = %v, want awaiting answer", m.state)
	}
	return m, reply
}

// The model writes the options, so a question can only offer answers it already
// thought of. There must always be one more the user can reach for.
func TestAskAlwaysOffersSomethingElse(t *testing.T) {
	m, _ := askTwo(t)
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "3) "+otherAnswerLabel) {
		t.Errorf("no escape-hatch option offered:\n%s", out)
	}
	// And the prompt has to say it is there, or nobody will find it.
	if bottom := stripANSI(m.bottomView()); !strings.Contains(bottom, "Choose 1-3") {
		t.Errorf("prompt does not count the extra option: %q", bottom)
	}
}

// Choosing it opens free text, and what the user types is what the tool gets —
// not the nearest label.
func TestSomethingElseSendsFreeText(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	if m.state != stateAnsweringOther {
		t.Fatalf("state = %v, want answering other", m.state)
	}
	typeRunes(m, "neither - trace the blast radius first")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case got := <-reply:
		if got != "neither - trace the blast radius first" {
			t.Errorf("answer = %q", got)
		}
	default:
		t.Fatal("no answer delivered to the waiting tool")
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want running after answering", m.state)
	}
}

// Reaching for the keyboard to say "none of these" shouldn't require finding a
// number first: the first character typed opens the box and is kept.
func TestTypingOpensFreeTextAndKeepsTheFirstCharacter(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.state != stateAnsweringOther {
		t.Fatalf("typing did not open free text (state %v)", m.state)
	}
	typeRunes(m, "one of these")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-reply; got != "none of these" {
		t.Errorf("answer = %q, want the first character preserved", got)
	}
}

// Esc is a way back, not a way out: the question is still live.
func TestEscReturnsToTheOptionList(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	typeRunes(m, "changed my mind")
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})

	if m.state != stateAwaitingAnswer {
		t.Fatalf("state = %v, want back at the options", m.state)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("draft survived the cancel: %q", v)
	}
	// The numbered options still work afterwards.
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if got := <-reply; got != "Version skew" {
		t.Errorf("answer = %q, want the second label", got)
	}
}

// The plain path must keep working exactly as before.
func TestNumberedOptionStillSelectsItsLabel(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if got := <-reply; got != "Explain the constraint" {
		t.Errorf("answer = %q", got)
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want running", m.state)
	}
}

// A digit past the end is a typo, not an answer — it must not be guessed at.
func TestDigitPastTheListIsIgnored(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("7")})
	if m.state != stateAwaitingAnswer {
		t.Errorf("state = %v, want still awaiting", m.state)
	}
	select {
	case got := <-reply:
		t.Errorf("an out-of-range digit answered the question: %q", got)
	default:
	}
}

// An interrupted turn clears the reply channel. Answering after that must not
// send on a nil channel, which blocks forever and freezes the UI.
func TestAnsweringAfterTheTurnEndedDoesNotHang(t *testing.T) {
	m, _ := askTwo(t)
	m.askReply = nil // as doneMsg does when a turn is interrupted mid-question
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.answerAsk("too late")
	}()
	select {
	case <-done:
	case <-timeoutAfter():
		t.Fatal("answering a dead question blocked; the UI would freeze")
	}
}

func timeoutAfter() <-chan time.Time { return time.After(2 * time.Second) }
