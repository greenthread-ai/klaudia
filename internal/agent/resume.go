package agent

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread/klaudia/internal/session"
)

// MessagesFromEntries reconstructs the conversation as API message params from
// transcript entries, for resuming a session. Each entry's stored message JSON
// is parsed into a BetaMessageParam (extra envelope fields like id/usage on
// assistant messages are ignored).
func MessagesFromEntries(entries []session.Entry) ([]anthropic.BetaMessageParam, error) {
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
