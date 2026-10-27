package session

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// Messages reconstructs the conversation as API message params from transcript
// entries, for resuming a session. Each entry's stored message JSON is parsed
// into a BetaMessageParam (extra envelope fields like id/usage on assistant
// messages are ignored).
//
// It lives here, beside the format it reads, rather than in internal/agent: the
// loop takes messages, and which file they came from is the product's business.
func Messages(entries []Entry) ([]anthropic.BetaMessageParam, error) {
	msgs := make([]anthropic.BetaMessageParam, 0, len(entries))
	for i, e := range entries {
		var m anthropic.BetaMessageParam
		if err := json.Unmarshal(e.Message, &m); err != nil {
			return nil, fmt.Errorf("entry %d (%s %s): %w", i, e.Type, e.UUID, err)
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
