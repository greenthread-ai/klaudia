package session

import (
	"encoding/json"
	"strconv"

	"github.com/google/uuid"
)

// Transcript is a stateful writer that turns role+message pairs into properly
// chained transcript entries (uuid/parentUuid) and appends them. It implements
// the agent.Recorder contract.
type Transcript struct {
	w        *Writer
	meta     Meta
	lastUUID *string
}

// Meta is the per-session metadata stamped onto every entry.
type Meta struct {
	SessionID      string
	CWD            string
	Version        string
	GitBranch      string
	PermissionMode string
	UserType       string // defaults to "external"

	// Resolved model sizing, stamped onto assistant entries by Record.
	//
	// Held per-entry rather than written once as a session header because
	// /model switches the model mid-session: a header would be correct for the
	// first turn and a lie for every turn after the first switch. Version and
	// GitBranch already repeat on every line for the same reason.
	//
	// Plain scalars rather than an api.ModelLimits so session stays a leaf
	// package — the same call doctor.floorOutputTokens makes.
	Model               string
	MaxTokens           int
	MaxTokensSource     string
	ContextWindow       int
	ContextWindowSource string
}

// NewTranscript opens the transcript for a session and returns a recorder.
func NewTranscript(meta Meta) (*Transcript, error) {
	if meta.UserType == "" {
		meta.UserType = "external"
	}
	w, err := NewWriter(meta.CWD, meta.SessionID)
	if err != nil {
		return nil, err
	}
	return &Transcript{w: w, meta: meta}, nil
}

// Record appends one message (role "user" or "assistant") to the transcript,
// chaining parentUuid to the previous entry.
func (t *Transcript) Record(role string, message json.RawMessage) error {
	id := uuid.NewString()
	e := Entry{
		Type:        role,
		UUID:        id,
		ParentUUID:  t.lastUUID,
		SessionID:   t.meta.SessionID,
		Timestamp:   Now(),
		CWD:         t.meta.CWD,
		GitBranch:   t.meta.GitBranch,
		IsSidechain: false,
		UserType:    t.meta.UserType,
		Version:     t.meta.Version,
		Message:     message,
	}
	if role == "user" {
		e.PermissionMode = t.meta.PermissionMode
	}
	if role == "assistant" {
		e.Model = t.meta.Model
		e.MaxTokens = t.meta.MaxTokens
		e.MaxTokensSource = t.meta.MaxTokensSource
		e.ContextWindow = t.meta.ContextWindow
		e.ContextWindowSource = t.meta.ContextWindowSource
	}
	t.lastUUID = &id
	return t.w.Append(e)
}

// RecordRewind appends a rewind boundary: the conversation was cut back to
// its first keep messages (the summary message counted, when there is one).
// Later entries chain from it, so they follow the cut on resume.
func (t *Transcript) RecordRewind(keep int) error {
	id := uuid.NewString()
	e := Entry{
		Type:       "system",
		Subtype:    SubtypeRewind,
		Content:    strconv.Itoa(keep),
		UUID:       id,
		ParentUUID: t.lastUUID,
		SessionID:  t.meta.SessionID,
		Timestamp:  Now(),
		CWD:        t.meta.CWD,
		GitBranch:  t.meta.GitBranch,
		UserType:   t.meta.UserType,
		Version:    t.meta.Version,
	}
	t.lastUUID = &id
	return t.w.Append(e)
}

// RecordCompaction appends a compaction boundary carrying summary: the point
// where the live conversation was replaced by it. ReadForResume rebuilds from
// the last one, so a resumed session is the summary plus what came after,
// not the summary alone.
//
// The summary goes in the transcript rather than relying on .summary.md for
// the same reason this is per transcript at all: the summary file is keyed by
// the process's session id, and an ACP process records many sessions.
func (t *Transcript) RecordCompaction(summary string) error {
	id := uuid.NewString()
	e := Entry{
		Type:       "system",
		Subtype:    SubtypeCompactBoundary,
		Content:    summary,
		UUID:       id,
		ParentUUID: t.lastUUID,
		SessionID:  t.meta.SessionID,
		Timestamp:  Now(),
		CWD:        t.meta.CWD,
		GitBranch:  t.meta.GitBranch,
		UserType:   t.meta.UserType,
		Version:    t.meta.Version,
	}
	t.lastUUID = &id
	return t.w.Append(e)
}

// SetSizing records the resolved model sizing stamped onto subsequent
// assistant entries. Safe to never call: the fields are omitempty, so a
// transcript that never learns its sizing writes exactly the bytes it wrote
// before these fields existed, which is what keeps old transcripts and
// --resume unaffected.
//
// Called per turn rather than at construction because /model can change the
// answer between turns.
func (t *Transcript) SetSizing(model string, maxTokens int, maxTokensSource string, contextWindow int, contextWindowSource string) {
	t.meta.Model = model
	t.meta.MaxTokens = maxTokens
	t.meta.MaxTokensSource = maxTokensSource
	t.meta.ContextWindow = contextWindow
	t.meta.ContextWindowSource = contextWindowSource
}

// Path returns the transcript file path.
func (t *Transcript) Path() string { return t.w.Path() }

// Close closes the underlying file.
func (t *Transcript) Close() error { return t.w.Close() }
