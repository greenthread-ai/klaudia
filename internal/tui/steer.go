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
type steerBox struct {
	mu   sync.Mutex
	text string
	halt bool
}

// add appends what the user typed. Two interjections before the agent looks are
// joined rather than one overwriting the other — losing the first would be a
// silent failure of the thing this exists to provide.
func (b *steerBox) add(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.text == "" {
		b.text = text
		return
	}
	b.text += "\n" + text
}

// halt asks Klaudia to stop after the current step.
func (b *steerBox) requestHalt() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.halt = true
}

// drain takes everything pending.
func (b *steerBox) drain() agent.Interjection {
	b.mu.Lock()
	defer b.mu.Unlock()
	in := agent.Interjection{Text: b.text, Halt: b.halt}
	b.text, b.halt = "", false
	return in
}

// peek reports what is pending, for the status hint. Does not consume.
func (b *steerBox) peek() (text string, halt bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text, b.halt
}

// pending reports whether anything is waiting.
func (b *steerBox) pending() bool {
	text, halt := b.peek()
	return text != "" || halt
}

// takeBack removes the pending text and returns it, for ↑ to edit.
func (b *steerBox) takeBack() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.text
	b.text = ""
	return t
}

// peekSteer is a test helper: the pending interjection text.
func peekSteer(m *Model) string {
	t, _ := m.steer.peek()
	return t
}
