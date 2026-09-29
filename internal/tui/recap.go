package tui

import (
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// A resumed session re-prints the tail of its conversation. The model already has the
// history; without this
// the person resuming — at a new terminal, or a browser that re-attached to a restarted
// process — sees an empty screen and a "Resuming" banner, and has to /export to remember
// what was said. Scrollback is immutable, so this is a recap, not a replay: user prompts
// and assistant text as they were, tool calls as one line each, tool results omitted.
const (
	resumeRecapMessages = 12 // most recent messages re-printed
	resumeRecapLines    = 20 // per text block; longer ones end in "…"
)

// summarySeed is how the CLI seeds a token-saving resume (root.go): one user message that
// carries the compacted summary rather than the conversation.
const summarySeed = "Summary of the earlier conversation in this session:"

// appendResumeRecap commits the recap to scrollback. A no-op for a fresh session.
func (m *Model) appendResumeRecap(history []anthropic.BetaMessageParam) {
	if len(history) == 0 {
		return
	}
	if len(history) == 1 && strings.HasPrefix(messageText(history[0]), summarySeed) {
		m.appendLine(hintStyle.Render("Resumed from the compacted summary — start with --full to see the whole conversation."))
		return
	}
	start := len(history) - resumeRecapMessages
	if start < 0 {
		start = 0
	}
	// Never open mid-exchange: begin at a prompt the person typed.
	for start < len(history) && !isPrompt(history[start]) {
		start++
	}
	if start == len(history) {
		return
	}
	if start > 0 {
		m.appendLine(hintStyle.Render(fmt.Sprintf("… %d earlier message(s) — /export for the whole conversation", start)))
	}
	for _, msg := range history[start:] {
		for _, block := range msg.Content {
			switch {
			case block.OfText != nil && msg.Role == anthropic.BetaMessageParamRoleUser:
				m.appendLine(userStyle.Render("› ") + clip(block.OfText.Text))
			case block.OfText != nil:
				if text := strings.TrimSpace(block.OfText.Text); text != "" {
					m.appendLine(clip(text))
				}
			case block.OfToolUse != nil:
				m.appendLine(toolStyle.Render("⏺ " + block.OfToolUse.Name))
			}
		}
	}
	m.appendLine(hintStyle.Render("── resumed ──"))
}

// isPrompt reports whether a message is one the person typed (a user message with text,
// not the tool results the loop sends back as user messages).
func isPrompt(msg anthropic.BetaMessageParam) bool {
	if msg.Role != anthropic.BetaMessageParamRoleUser {
		return false
	}
	for _, block := range msg.Content {
		if block.OfText != nil && strings.TrimSpace(block.OfText.Text) != "" {
			return true
		}
	}
	return false
}

func messageText(msg anthropic.BetaMessageParam) string {
	for _, block := range msg.Content {
		if block.OfText != nil {
			return block.OfText.Text
		}
	}
	return ""
}

// clip keeps a recap block to resumeRecapLines lines.
func clip(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= resumeRecapLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[:resumeRecapLines], "\n") + "\n…"
}
