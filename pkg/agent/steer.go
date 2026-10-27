package agent

import (
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
)

// Steering: a correction should land before the next consequential action.
//
// Typing "don't modify the API" while Klaudia works used to hold the text until
// the turn ended, then send it as a new turn. By then the API had been
// modified. The restriction arrived as a complaint rather than an instruction.
//
// The fix is small: the loop asks, between turns and after each tool batch,
// whether the user has said anything. Anything they have said becomes a user
// message on the very next request — so the model sees it before it decides
// what to do next, which is the only placement that makes "don't" mean
// anything.
//
// It is deliberately a poll rather than a channel the loop selects on. The loop
// has exactly two points where injecting a message is safe: before a request is
// built, and after a tool batch's results are appended. Anywhere else would
// interleave a user message with a tool_use/tool_result pair and the API would
// reject the conversation.

// Interjection is what the user said while Klaudia was working.
type Interjection struct {
	// Text is the instruction, empty if they only asked to stop.
	Text string
	// Halt asks Klaudia to finish the step it is on and then stop, rather than
	// abandoning it. "Stop after this test run" should not throw away the test
	// run.
	Halt bool
}

// Empty reports whether there is nothing to inject.
func (i Interjection) Empty() bool { return strings.TrimSpace(i.Text) == "" && !i.Halt }

// haltInstruction is appended when the user asks to stop after the current
// step. It tells the model to wrap up rather than fall silent, because a turn
// that just ends leaves the user to work out what was finished.
const haltInstruction = "The user asked you to stop after the current step. Do not start anything new. " +
	"Summarise what you completed, what you were part-way through, and what remains."

// The framings steerMessage puts round an interjection's text.
const (
	// A report from work the model started (a job exiting, a background agent
	// finishing), not something the user said. Framed as an interruption it
	// read as a new order; it is news to act on.
	backgroundFrame = "Background work you started has reported back. Take it into account " +
		"for what you do next:\n\n"
	// So the model treats it as a live instruction rather than as
	// conversational filler arriving mid-task. Without the framing, an
	// interjection reads like an aside and gets acknowledged instead of
	// obeyed.
	userFrame = "The user interrupted with a new instruction. It applies from now on, " +
		"including to anything you were about to do:\n\n"
)

// steerMessage renders an interjection as a user message.
func steerMessage(in Interjection) (anthropic.BetaMessageParam, bool) {
	var parts []string
	if t := strings.TrimSpace(in.Text); t != "" && strings.HasPrefix(t, "<background-") {
		parts = append(parts, backgroundFrame+t)
	} else if t != "" {
		parts = append(parts, userFrame+t)
	}
	if in.Halt {
		parts = append(parts, haltInstruction)
	}
	if len(parts) == 0 {
		return anthropic.BetaMessageParam{}, false
	}
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(strings.Join(parts, "\n\n"))), true
}

// ParseInterjection recovers the interjection steerMessage rendered as text.
// It is for a reader of a transcript — session replay — which has to tell a
// message the user sent mid-turn from a new prompt: both are user text after
// tool results. ok is false for text steerMessage did not write.
func ParseInterjection(text string) (in Interjection, ok bool) {
	if text == haltInstruction {
		return Interjection{Halt: true}, true
	}
	if rest, found := strings.CutSuffix(text, "\n\n"+haltInstruction); found {
		text, in.Halt = rest, true
	}
	switch {
	case strings.HasPrefix(text, userFrame):
		in.Text = strings.TrimPrefix(text, userFrame)
	case strings.HasPrefix(text, backgroundFrame):
		in.Text = strings.TrimPrefix(text, backgroundFrame)
	default:
		return Interjection{}, false
	}
	return in, true
}

// Inbox queues reports for a running turn — a background job's exit, say —
// from goroutines the loop does not own, and hands them over through
// Turn.Interject at the loop's next safe point. It is for frontends that have
// no steering box of their own (headless, stream-json). Safe for concurrent
// use; the zero value is ready.
type Inbox struct {
	mu    sync.Mutex
	items []string
}

// Add queues a report.
func (b *Inbox) Add(text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, text)
}

// Interject drains the queue as one interjection; use it as Turn.Interject.
func (b *Inbox) Interject() Interjection {
	b.mu.Lock()
	defer b.mu.Unlock()
	in := Interjection{Text: strings.Join(b.items, "\n\n")}
	b.items = nil
	return in
}

// pollInterjection reads pending user input, if the caller wired a source.
func pollInterjection(opts Options) Interjection {
	if opts.Interject == nil {
		return Interjection{}
	}
	return opts.Interject()
}
