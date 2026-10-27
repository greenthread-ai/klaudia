package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// askTwo puts a two-option question to the model under test and returns the
// reply channel the tool would be blocking on.
func askTwo(t *testing.T) (*Model, chan string) {
	t.Helper()
	m := newTestModel()
	m.resize(100, 30)
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
	if bottom := stripANSI(m.bottomView()); !strings.Contains(bottom, "3) "+otherAnswerLabel) || !strings.Contains(bottom, "1-3 to pick") {
		t.Errorf("no escape-hatch option offered, or the hint does not count it:\n%s", bottom)
	}
	// The options are a live picker; scrollback keeps only the question.
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "? Which problem") || strings.Contains(out, "Version skew") {
		t.Errorf("scrollback should hold the question and not the options:\n%s", out)
	}
}

// Choosing it opens free text, and what the user types is what the tool gets —
// not the nearest label.
func TestSomethingElseSendsFreeText(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(textKey("3"))
	if m.state != stateAnsweringOther {
		t.Fatalf("state = %v, want answering other", m.state)
	}
	typeRunes(m, "neither - trace the blast radius first")
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEnter})

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

// The audit failure: a message typed ahead while the model worked answered
// the question that appeared under it. Text must not answer, and the Enter that
// ends it must not confirm the highlighted row either.
func TestTypingAheadDoesNotAnswer(t *testing.T) {
	m, reply := askTwo(t)
	typeRunes(m, "also fix the tests")
	if !strings.Contains(stripANSI(m.bottomView()), "typing doesn't answer") {
		t.Error("ignored typing should be explained")
	}
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	select {
	case got := <-reply:
		t.Fatalf("typed-ahead text answered the question: %q", got)
	default:
	}
	if m.state != stateAwaitingAnswer {
		t.Fatalf("state = %v, want still awaiting", m.state)
	}
	// The next Enter is deliberate and takes the highlighted row.
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := <-reply; got != "Explain the constraint" {
		t.Errorf("answer = %q, want the first row", got)
	}
}

// Arrow keys move the highlight, wrapping, and Enter takes it — including the
// escape hatch, which opens free text rather than answering.
func TestArrowsAndEnterPick(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := <-reply; got != "Version skew" {
		t.Errorf("answer = %q, want the second row", got)
	}

	m, _ = askTwo(t)
	m.onKey(tea.KeyPressMsg{Code: tea.KeyUp}) // wraps to the escape hatch
	if !strings.Contains(stripANSI(m.bottomView()), "❯ 3) "+otherAnswerLabel) {
		t.Errorf("up from the top should wrap to the last row:\n%s", stripANSI(m.bottomView()))
	}
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.state != stateAnsweringOther {
		t.Errorf("state = %v, want answering other", m.state)
	}
}

// Esc is a way back, not a way out: the question is still live.
func TestEscReturnsToTheOptionList(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(textKey("3"))
	typeRunes(m, "changed my mind")
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEsc})

	if m.state != stateAwaitingAnswer {
		t.Fatalf("state = %v, want back at the options", m.state)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("draft survived the cancel: %q", v)
	}
	// The numbered options still work afterwards.
	m.onKey(textKey("2"))
	if got := <-reply; got != "Version skew" {
		t.Errorf("answer = %q, want the second label", got)
	}
}

// The plain path must keep working exactly as before.
func TestNumberedOptionStillSelectsItsLabel(t *testing.T) {
	m, reply := askTwo(t)
	m.onKey(textKey("1"))
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
	m.onKey(textKey("7"))
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
