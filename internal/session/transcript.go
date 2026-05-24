package session

import (
	"encoding/json"

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
	t.lastUUID = &id
	return t.w.Append(e)
}

// Path returns the transcript file path.
func (t *Transcript) Path() string { return t.w.Path() }

// Close closes the underlying file.
func (t *Transcript) Close() error { return t.w.Close() }
