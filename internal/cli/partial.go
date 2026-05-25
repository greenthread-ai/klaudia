package cli

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
)

// partialEmitter writes raw model stream events as JS-compatible stream_event
// lines: {type:"stream_event", event:<raw SSE event>, session_id,
// parent_tool_use_id, uuid} (07-app-features.js:33560). It is wired to
// agent.Options.PartialMessages when --include-partial-messages is set. Writes
// are mutex-guarded so partials interleave safely with the envelope recorder
// writing to the same stream.
type partialEmitter struct {
	w         io.Writer
	sessionID string
	mu        *sync.Mutex
}

func newPartialEmitter(w io.Writer, sessionID string, mu *sync.Mutex) *partialEmitter {
	return &partialEmitter{w: w, sessionID: sessionID, mu: mu}
}

// emit serializes one raw stream event. The event's unmodified API JSON
// (RawJSON) is forwarded verbatim as the "event" field, so the line is
// structurally identical to the JS reference's partial output.
func (e *partialEmitter) emit(ev anthropic.BetaRawMessageStreamEventUnion) {
	raw := ev.RawJSON()
	if raw == "" {
		return
	}
	line, err := json.Marshal(map[string]any{
		"type":               "stream_event",
		"event":              json.RawMessage(raw),
		"session_id":         e.sessionID,
		"parent_tool_use_id": nil,
		"uuid":               uuid.NewString(),
	})
	if err != nil {
		return
	}
	if e.mu != nil {
		e.mu.Lock()
		defer e.mu.Unlock()
	}
	_, _ = e.w.Write(append(line, '\n'))
}
