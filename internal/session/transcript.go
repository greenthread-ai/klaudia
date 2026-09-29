package session

import (
	"bytes"
	"encoding/json"
	"os"

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

// DropLastMessages truncates the transcript in place, removing the last n
// user/assistant message entries (and any trailing non-message lines after the
// last kept message) so a rewound conversation resumes to match what is on
// screen. It returns the number of message entries actually removed, capped at
// the total present. n<=0 is a no-op.
//
// The truncation is done with os.Truncate on the existing path, so it keeps the
// same inode: an append handle this Transcript already opened stays valid and
// its next Record writes at the new end of file. The recorder's parent-chain
// pointer is re-seated on the last surviving entry so a resumed chain stays
// intact.
func (t *Transcript) DropLastMessages(n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	dropped, lastUUID, err := truncateTranscript(t.w.Path(), n)
	if err != nil {
		return 0, err
	}
	if dropped > 0 {
		t.lastUUID = lastUUID
	}
	return dropped, nil
}

// truncateTranscript rewrites path in place, dropping the last n message entries
// (Type "user"/"assistant"). It returns the number dropped and the uuid of the
// new last message entry (nil when none remain), for the recorder's parent
// chain. A path that does not exist yet (nothing has been recorded) is a no-op.
func truncateTranscript(path string, n int) (dropped int, lastUUID *string, err error) {
	if n <= 0 {
		return 0, nil, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil, nil // never appended: nothing to drop
	}
	if err != nil {
		return 0, nil, err
	}

	// Record, for each user/assistant message line, the byte offset just past
	// its trailing newline and its uuid. Non-message lines (queue ops, titles,
	// attachments) are not boundaries and are carried along with the message
	// that precedes them.
	type msgLine struct {
		end  int
		uuid string
	}
	var msgs []msgLine
	offset := 0
	for offset < len(data) {
		nl := bytes.IndexByte(data[offset:], '\n')
		lineEnd := len(data)
		if nl >= 0 {
			lineEnd = offset + nl + 1 // include the newline
		}
		line := data[offset:lineEnd]
		if len(bytes.TrimSpace(line)) > 0 {
			var e struct {
				Type string `json:"type"`
				UUID string `json:"uuid"`
			}
			if json.Unmarshal(bytes.TrimSpace(line), &e) == nil && (e.Type == "user" || e.Type == "assistant") {
				msgs = append(msgs, msgLine{end: lineEnd, uuid: e.UUID})
			}
		}
		offset = lineEnd
	}

	total := len(msgs)
	keep := total - n
	if keep < 0 {
		keep = 0
	}
	dropped = total - keep
	if dropped == 0 {
		if total > 0 {
			id := msgs[total-1].uuid
			return 0, &id, nil
		}
		return 0, nil, nil
	}

	cut := 0
	if keep > 0 {
		cut = msgs[keep-1].end
		id := msgs[keep-1].uuid
		lastUUID = &id
	}
	if err := os.Truncate(path, int64(cut)); err != nil {
		return 0, nil, err
	}
	return dropped, lastUUID, nil
}

// Close closes the underlying file.
func (t *Transcript) Close() error { return t.w.Close() }
