package agent

import (
	"strings"

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

// steerMessage renders an interjection as a user message.
func steerMessage(in Interjection) (anthropic.BetaMessageParam, bool) {
	var parts []string
	if t := strings.TrimSpace(in.Text); t != "" {
		// Framed so the model treats it as a live instruction rather than as
		// conversational filler arriving mid-task. Without the framing, an
		// interjection reads like an aside and gets acknowledged instead of
		// obeyed.
		parts = append(parts, "The user interrupted with a new instruction. It applies from now on, "+
			"including to anything you were about to do:\n\n"+t)
	}
	if in.Halt {
		parts = append(parts, haltInstruction)
	}
	if len(parts) == 0 {
		return anthropic.BetaMessageParam{}, false
	}
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(strings.Join(parts, "\n\n"))), true
}

// pollInterjection reads pending user input, if the caller wired a source.
func pollInterjection(opts Options) Interjection {
	if opts.Interject == nil {
		return Interjection{}
	}
	return opts.Interject()
}
