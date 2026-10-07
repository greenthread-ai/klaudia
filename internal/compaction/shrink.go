package compaction

import (
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// minShrinkChars is the text-block size below which ShrinkForSummary stops
// truncating: a conversation still over the limit once every block is this
// short is not one a summary can rescue.
const minShrinkChars = 2000

// ShrinkForSummary makes a conversation smaller for a summary request that was
// itself refused as too long. The summary request is the whole history plus an
// instruction, so when the history has already overflowed the window, sending
// it again as-is overflows the same way — and compaction, the only way out,
// failed silently every time.
//
// It drops the older half, cutting at a real user turn (one carrying no
// tool_result, so no result is left without its tool_use). When there is too
// little to drop — a short conversation, or one enormous first prompt — it
// halves the longest text blocks instead, keeping each one's head and tail.
// dropped is the number of messages removed; ok is false when neither step
// could make the conversation smaller.
func ShrinkForSummary(messages []anthropic.BetaMessageParam) (out []anthropic.BetaMessageParam, dropped int, ok bool) {
	if cut := userTurnCut(messages); cut > 0 {
		return messages[cut:], cut, true
	}
	longest := 0
	for _, m := range messages {
		for _, b := range m.Content {
			if b.OfText != nil && len(b.OfText.Text) > longest {
				longest = len(b.OfText.Text)
			}
		}
	}
	if longest <= minShrinkChars {
		return messages, 0, false
	}
	limit := longest / 2
	out = make([]anthropic.BetaMessageParam, len(messages))
	for i, m := range messages {
		out[i] = m
		copied := false // the input's content slices are never written
		for j, b := range m.Content {
			if b.OfText == nil || len(b.OfText.Text) <= limit {
				continue
			}
			if !copied {
				out[i].Content = append([]anthropic.BetaContentBlockParamUnion{}, m.Content...)
				copied = true
			}
			out[i].Content[j] = anthropic.NewBetaTextBlock(headTail(b.OfText.Text, limit))
		}
	}
	return out, 0, true
}

// userTurnCut returns the index of the first real user turn at or after the
// middle of messages, or failing that before it (but not the first message),
// or 0 when there is none.
func userTurnCut(messages []anthropic.BetaMessageParam) int {
	if len(messages) < 3 {
		return 0
	}
	isTurn := func(m anthropic.BetaMessageParam) bool {
		if m.Role != anthropic.BetaMessageParamRoleUser {
			return false
		}
		for _, b := range m.Content {
			if b.OfToolResult != nil {
				return false
			}
		}
		return true
	}
	mid := len(messages) / 2
	for i := mid; i < len(messages); i++ {
		if isTurn(messages[i]) {
			return i
		}
	}
	for i := mid - 1; i > 0; i-- {
		if isTurn(messages[i]) {
			return i
		}
	}
	return 0
}

// headTail keeps the first and last limit/2 bytes of s, on rune boundaries,
// with a marker saying how much was removed between them.
func headTail(s string, limit int) string {
	head, tail := limit/2, limit/2
	for head > 0 && head < len(s) && !runeStart(s[head]) {
		head--
	}
	start := len(s) - tail
	for start < len(s) && !runeStart(s[start]) {
		start++
	}
	return s[:head] + fmt.Sprintf("\n[… %d characters removed to fit the summary request …]\n", start-head) + s[start:]
}

func runeStart(b byte) bool { return b&0xC0 != 0x80 }
