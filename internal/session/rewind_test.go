package session

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// recordN writes count alternating user/assistant message entries to a fresh
// transcript and returns it plus its path.
func recordN(t *testing.T, count int) (*Transcript, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	tr, err := NewTranscript(Meta{SessionID: "s", CWD: "/x", Path: path})
	if err != nil {
		t.Fatalf("NewTranscript: %v", err)
	}
	for i := 0; i < count; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msg, _ := json.Marshal(map[string]any{"role": role, "content": "m"})
		if err := tr.Record(role, msg); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	return tr, path
}

func countMessages(t *testing.T, path string) int {
	t.Helper()
	entries, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return len(entries)
}

func TestDropLastMessagesTruncates(t *testing.T) {
	tr, path := recordN(t, 6)
	dropped, err := tr.DropLastMessages(4)
	if err != nil {
		t.Fatalf("DropLastMessages: %v", err)
	}
	if dropped != 4 {
		t.Fatalf("dropped = %d, want 4", dropped)
	}
	if got := countMessages(t, path); got != 2 {
		t.Fatalf("remaining messages = %d, want 2", got)
	}
}

func TestDropLastMessagesAppendChainsFromSurvivor(t *testing.T) {
	// After a truncation the recorder must keep appending onto the surviving
	// tail, and the parentUuid of the next entry must point at the last kept
	// entry — not a dropped one.
	tr, path := recordN(t, 4)
	entries, _ := Read(path)
	survivorUUID := entries[1].UUID // we will keep the first 2

	if _, err := tr.DropLastMessages(2); err != nil {
		t.Fatalf("DropLastMessages: %v", err)
	}
	msg, _ := json.Marshal(map[string]any{"role": "user", "content": "next"})
	if err := tr.Record("user", msg); err != nil {
		t.Fatalf("Record after truncate: %v", err)
	}

	entries, _ = Read(path)
	if len(entries) != 3 {
		t.Fatalf("entries after append = %d, want 3", len(entries))
	}
	last := entries[len(entries)-1]
	if last.ParentUUID == nil || *last.ParentUUID != survivorUUID {
		t.Fatalf("appended entry parent = %v, want %q", last.ParentUUID, survivorUUID)
	}
}

func TestDropLastMessagesExceedsAvailable(t *testing.T) {
	tr, path := recordN(t, 3)
	dropped, err := tr.DropLastMessages(10)
	if err != nil {
		t.Fatalf("DropLastMessages: %v", err)
	}
	if dropped != 3 {
		t.Fatalf("dropped = %d, want 3 (all)", dropped)
	}
	if got := countMessages(t, path); got != 0 {
		t.Fatalf("remaining messages = %d, want 0", got)
	}
}

func TestDropLastMessagesNonPositiveNoOp(t *testing.T) {
	tr, path := recordN(t, 3)
	for _, n := range []int{0, -1} {
		dropped, err := tr.DropLastMessages(n)
		if err != nil || dropped != 0 {
			t.Fatalf("n=%d: dropped=%d err=%v, want 0/nil", n, dropped, err)
		}
	}
	if got := countMessages(t, path); got != 3 {
		t.Fatalf("no-op changed the file: messages = %d, want 3", got)
	}
}

func TestDropLastMessagesNoFileYet(t *testing.T) {
	// A transcript that has recorded nothing has no file on disk; truncation is a
	// clean no-op rather than an error.
	path := filepath.Join(t.TempDir(), "s.jsonl")
	tr, err := NewTranscript(Meta{SessionID: "s", CWD: "/x", Path: path})
	if err != nil {
		t.Fatalf("NewTranscript: %v", err)
	}
	dropped, err := tr.DropLastMessages(2)
	if err != nil || dropped != 0 {
		t.Fatalf("dropped=%d err=%v, want 0/nil", dropped, err)
	}
}
