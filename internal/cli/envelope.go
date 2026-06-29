package cli

import (
	"encoding/json"
	"io"

	"github.com/google/uuid"

	"github.com/greenthread-ai/klaudia/internal/agent"
)

// envelopeRecorder emits each conversation message as a JS-compatible
// stream-json line: {type, message, session_id, parent_tool_use_id, uuid}
// (07-app-features.js:33435). This gives Zed/SDK/ACP consumers the same
// envelope Claude Code produces. It implements agent.Recorder.
type envelopeRecorder struct {
	w         io.Writer
	sessionID string
}

func newEnvelopeRecorder(w io.Writer, sessionID string) *envelopeRecorder {
	return &envelopeRecorder{w: w, sessionID: sessionID}
}

func (e *envelopeRecorder) Record(role string, message json.RawMessage) error {
	line, err := json.Marshal(map[string]any{
		"type":               role, // "user" | "assistant"
		"message":            message,
		"session_id":         e.sessionID,
		"parent_tool_use_id": nil,
		"uuid":               uuid.NewString(),
	})
	if err != nil {
		return err
	}
	_, err = e.w.Write(append(line, '\n'))
	return err
}

// multiRecorder fans a Record call out to several recorders (e.g. the transcript
// writer plus the stream-json envelope emitter). A nil entry is skipped.
type multiRecorder []agent.Recorder

func (m multiRecorder) Record(role string, message json.RawMessage) error {
	for _, r := range m {
		if r == nil {
			continue
		}
		if err := r.Record(role, message); err != nil {
			return err
		}
	}
	return nil
}
