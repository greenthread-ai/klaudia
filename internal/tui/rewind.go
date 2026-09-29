package tui

import (
	"fmt"
	"strconv"

	"github.com/anthropics/anthropic-sdk-go"
)

// isUserPrompt reports whether msg is a genuine user prompt — the start of an
// exchange — as opposed to the user-role message the loop uses to carry a
// tool_result back to the model.
//
// The distinction matters because the Anthropic API delivers tool results as
// user-role messages containing tool_result blocks. Treating one of those as an
// exchange boundary and cutting in front of it would strand the preceding
// assistant tool_use with no result, which is exactly the corruption
// sanitizeMessages (internal/agent) has to repair. So a user message that
// carries any tool_result block is never a boundary; only a user message with
// none is.
func isUserPrompt(msg anthropic.BetaMessageParam) bool {
	if msg.Role != anthropic.BetaMessageParamRoleUser {
		return false
	}
	for _, b := range msg.Content {
		if b.OfToolResult != nil {
			return false
		}
	}
	return true
}

// rewindHistory drops the last n exchanges from history and returns the kept
// prefix along with how many exchanges and messages were removed.
//
// An exchange begins at a user prompt (isUserPrompt) and runs up to the message
// before the next user prompt — i.e. the prompt plus the assistant turn(s) and
// tool results it triggered. Rewinding cuts the history at a user-prompt
// boundary, so every dropped exchange is removed whole: no tool_use is ever left
// without its tool_result (or vice versa), because a boundary is by definition a
// message that carries no tool_result. The kept prefix is returned untouched.
//
// n<=0 is a no-op (returns the input and 0, 0). When n exceeds the number of
// exchanges present, all of them are dropped and the true count is reported.
func rewindHistory(history []anthropic.BetaMessageParam, n int) (kept []anthropic.BetaMessageParam, exchanges, messages int) {
	if n <= 0 {
		return history, 0, 0
	}
	// Indices where each exchange starts.
	var starts []int
	for i, msg := range history {
		if isUserPrompt(msg) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return history, 0, 0
	}
	if n >= len(starts) {
		cut := starts[0]
		return history[:cut:cut], len(starts), len(history) - cut
	}
	cut := starts[len(starts)-n]
	return history[:cut:cut], n, len(history) - cut
}

// rewindCommand implements /rewind [N]: it removes the last N exchanges (default
// 1) from the in-memory conversation, truncates the persisted transcript to
// match, and records a divider in the scrollback. It runs only when idle.
func (m *Model) rewindCommand(args []string) {
	if m.busyGuard("/rewind") {
		return
	}

	n := 1
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v <= 0 {
			m.appendLine(errStyle.Render("usage: /rewind [N] — N is a positive whole number of exchanges to drop (default 1)"))
			return
		}
		n = v
	}

	// Count available exchanges up front so the message can be honest about how
	// many were actually dropped when N overshoots.
	available := 0
	for _, msg := range m.history {
		if isUserPrompt(msg) {
			available++
		}
	}
	if available == 0 {
		m.appendLine(bannerStyle.Render("Nothing to rewind — the conversation is empty."))
		return
	}

	kept, exchanges, dropped := rewindHistory(m.history, n)
	m.history = kept

	// Keep the persisted transcript in step so a later resume matches the
	// screen. Best effort: a transcript-write failure should not desync the
	// in-memory state the user can already see is gone.
	if m.sess != nil && m.sess.Rewind != nil && dropped > 0 {
		if err := m.sess.Rewind(dropped); err != nil {
			m.appendLine(errStyle.Render("rewind: transcript not truncated: " + err.Error()))
		}
	}

	noun := "exchange"
	if exchanges != 1 {
		noun = "exchanges"
	}
	msg := fmt.Sprintf("── Rewound %d %s (%d messages dropped). The conversation continues from here.", exchanges, noun, dropped)
	if exchanges < n {
		msg = fmt.Sprintf("── Rewound all %d %s (%d messages dropped); that was fewer than the %d requested. The conversation is now empty.", exchanges, noun, dropped, n)
	}
	m.appendLine(bannerStyle.Render(msg))
}
