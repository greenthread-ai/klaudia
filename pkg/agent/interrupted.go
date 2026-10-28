package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Markers closing a reply that stopped before it finished. They are recorded as
// the last block of the assistant turn, so the model knows on the next request
// that its previous answer was cut short and what the user actually saw.
const (
	interruptedByUserMarker = "[Interrupted by the user before this reply finished.]"
	interruptedStreamMarker = "[Cut off: the connection to the model ended before this reply finished. " +
		"The text above is all the user saw.]"
)

// interruptedPartial turns what a failed stream had already shown into an
// assistant message that can join the history. Without it the user has read
// the partial answer but the model has not — the next request carries no trace
// of it, so the model repeats itself or contradicts what it said.
//
// Only text survives. A tool_use in the partial message never ran and has no
// tool_result to pair with, so sending it back would be rejected; thinking
// blocks of an unfinished turn carry no signature. ok is false when no text was
// shown, in which case nothing is added: the history is exactly as it was
// before the request.
func interruptedPartial(m anthropic.BetaMessage, err error) (anthropic.BetaMessageParam, bool) {
	var blocks []anthropic.BetaContentBlockParamUnion
	for _, b := range m.Content {
		if b.Type != "text" {
			continue
		}
		// b.Text, not b.AsText(): AsText decodes the block's raw JSON, which the
		// SDK refreshes only at content_block_stop, so for a block the stream
		// never finished it is still the empty start event.
		if t := b.Text; strings.TrimSpace(t) != "" {
			blocks = append(blocks, anthropic.NewBetaTextBlock(t))
		}
	}
	if len(blocks) == 0 {
		return anthropic.BetaMessageParam{}, false
	}
	marker := interruptedStreamMarker
	if errors.Is(err, context.Canceled) {
		marker = interruptedByUserMarker
	}
	blocks = append(blocks, anthropic.NewBetaTextBlock(marker))
	return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: blocks}, true
}
