package cli

import (
	"encoding/json"
	"io"

	"github.com/google/uuid"

	"github.com/greenthread-ai/klaudia/pkg/agent"
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
//
// Every recorder is written even when an earlier one fails, and the first
// error is returned. Stopping at the first failure meant a full disk under
// the transcript also cut off the stream-json envelope an embedder reads,
// which is on stdout and has nothing to do with the disk. (From PR #44.)
type multiRecorder []agent.Recorder

// RecordCompaction forwards to every recorder that can mark a boundary. The
// headless path wraps the transcript in a multiRecorder, so without this the
// assertion in autocompact would fail and the boundary would never be written.
func (m multiRecorder) RecordCompaction(summary string) error {
	var first error
	for _, r := range m {
		if cr, ok := r.(agent.CompactionRecorder); ok {
			if err := cr.RecordCompaction(summary); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// RecordThinkingDropped forwards like RecordCompaction, for the same reason.
func (m multiRecorder) RecordThinkingDropped(note string) error {
	var first error
	for _, r := range m {
		if tr, ok := r.(agent.ThinkingDropRecorder); ok {
			if err := tr.RecordThinkingDropped(note); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func (m multiRecorder) Record(role string, message json.RawMessage) error {
	var first error
	for _, r := range m {
		if r == nil {
			continue
		}
		if err := r.Record(role, message); err != nil && first == nil {
			first = err
		}
	}
	return first
}
