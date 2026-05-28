package agent

import "github.com/anthropics/anthropic-sdk-go"

// emptyContentPlaceholder is what a message with no content blocks gets rewritten
// to before the request goes upstream. The Anthropic API rejects any message
// missing `content` (or with an empty array) — "messages.<i>.content: Field
// required" — so we must replace nil/empty with at least one block to keep the
// conversation sendable.
const emptyContentPlaceholder = "[Empty assistant turn — content was not recorded]"

// sanitizeMessages rewrites any message whose Content is nil or empty to a
// single text block, so the request never trips the Anthropic API's
// "content: Field required" 400. This covers, for example: an assistant turn
// the OpenAI-compatible shim returned with `content: null` (refusal), an
// interrupted turn recorded with no blocks, or any future bug producing the
// same shape. Non-empty messages are passed through untouched.
//
// Returned slice is independent of the input (a touched message gets a new
// Content slice; untouched messages share the input's structures).
func sanitizeMessages(messages []anthropic.BetaMessageParam) []anthropic.BetaMessageParam {
	// Fast path: nothing to fix, return the input unchanged.
	bad := false
	for _, m := range messages {
		if len(m.Content) == 0 {
			bad = true
			break
		}
	}
	if !bad {
		return messages
	}
	out := append([]anthropic.BetaMessageParam(nil), messages...)
	for i, m := range out {
		if len(m.Content) == 0 {
			out[i].Content = []anthropic.BetaContentBlockParamUnion{
				anthropic.NewBetaTextBlock(emptyContentPlaceholder),
			}
		}
	}
	return out
}
