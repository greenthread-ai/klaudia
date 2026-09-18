package tui

import (
	"strings"
	"sync"

	"github.com/greenthread-ai/klaudia/internal/agent"
)

// steerBox is the handoff between the two goroutines that both care about what
// the user typed mid-turn.
//
// The TUI writes to it from the Bubble Tea update loop; the agent drains it
// from its own goroutine at the two points in the loop where a user message can
// safely be appended. A plain string field would be a data race, and this is
// small enough that a mutex is the whole design.
//
// Draining, rather than reading, is what makes the rest work: once the agent
// has taken a message, the end-of-turn handler must not send it a second time
// as a fresh turn.
//
// It keeps the message in both forms, for the same reason the idle submit path
// does: display is the paste-chip form the user sees and can recall with ↑,
// prompt is the expanded payload the agent receives. Expansion happens here at
// add time rather than at drain, because the queued branch resets the input
// without pushing history — so the moment the box is cleared the chip is
// referenced nowhere, and the next reconcile would evict the payload out from
// under a later expansion.
type steerBox struct {
	mu      sync.Mutex
	display string
	prompt  string
	halt    bool
}

// add appends what the user typed, in both its displayed and expanded forms.
// Two interjections before the agent looks are joined rather than one
// overwriting the other — losing the first would be a silent failure of the
// thing this exists to provide.
func (b *steerBox) add(display, prompt string) {
	display, prompt = strings.TrimSpace(display), strings.TrimSpace(prompt)
	if display == "" {
		return
	}
	// A caller with nothing to expand passes the same text twice; tolerate an
	// empty prompt rather than queueing a message the agent will never see.
	if prompt == "" {
		prompt = display
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.display == "" {
		b.display, b.prompt = display, prompt
		return
	}
	b.display += "\n" + display
	b.prompt += "\n" + prompt
}

// halt asks Klaudia to stop after the current step.
func (b *steerBox) requestHalt() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.halt = true
}

// drain takes everything pending, as the expanded text the agent should read.
func (b *steerBox) drain() agent.Interjection {
	b.mu.Lock()
	defer b.mu.Unlock()
	in := agent.Interjection{Text: b.prompt, Halt: b.halt}
	b.display, b.prompt, b.halt = "", "", false
	return in
}

// peek reports what is pending, for the status hint. Does not consume. This is
// the display form: the hint has one line, and a pasted file does not fit in it.
func (b *steerBox) peek() (text string, halt bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.display, b.halt
}

// pending reports whether anything is waiting.
func (b *steerBox) pending() bool {
	text, halt := b.peek()
	return text != "" || halt
}

// takeBack removes the pending text and returns it, for ↑ to edit. The display
// form again: recalling a paste should put the chip back in the box, not a
// thousand lines of payload.
func (b *steerBox) takeBack() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.display
	b.display, b.prompt = "", ""
	return t
}

// peekSteer is a test helper: the pending interjection text, as displayed.
func peekSteer(m *Model) string {
	t, _ := m.steer.peek()
	return t
}

// peekSteerPrompt is a test helper: the pending text as the agent would read it.
func peekSteerPrompt(m *Model) string {
	m.steer.mu.Lock()
	defer m.steer.mu.Unlock()
	return m.steer.prompt
}
