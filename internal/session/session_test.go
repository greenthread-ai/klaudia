package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEncodePath is kept stable so existing Klaudia transcripts remain under the
// same project directory names when moving storage roots.
func TestEncodePathMatchesJS(t *testing.T) {
	got := EncodePath("/Users/nickglynn/Projects/claude-code/klaudia")
	want := "-Users-nickglynn-Projects-claude-code-klaudia"
	if got != want {
		t.Errorf("EncodePath = %q, want %q", got, want)
	}
}

func TestEncodePathLongTruncatesWithHash(t *testing.T) {
	long := "/" + strings.Repeat("a", 300)
	got := EncodePath(long)
	if len(got) <= maxDirLen {
		t.Errorf("expected truncation+hash for long path, got len %d", len(got))
	}
	if !strings.Contains(got, "-") {
		t.Errorf("expected hash suffix separated by '-', got %q", got)
	}
}

func TestDirUsesSessionsRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", root)
	cwd := "/work/proj"
	want := filepath.Join(root, "sessions", EncodePath(cwd))
	if got := Dir(cwd); got != want {
		t.Fatalf("Dir = %q, want %q", got, want)
	}
}

func TestExistingPathFallsBackToLegacyProjectsRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", root)
	cwd := "/work/proj"
	sid := "legacy-session"
	legacy := filepath.Join(root, "projects", EncodePath(cwd), sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ExistingPath(cwd, sid); got != legacy {
		t.Fatalf("ExistingPath = %q, want %q", got, legacy)
	}
}

func TestWriteReadRoundTrip(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/proj"
	sid := "sess-123"

	w, err := NewWriter(cwd, sid)
	if err != nil {
		t.Fatal(err)
	}
	parent := "u1"
	entries := []Entry{
		{
			Type: "user", UUID: "u1", SessionID: sid, Timestamp: Now(), CWD: cwd,
			UserType: "external", Version: "test", PermissionMode: "default",
			Message: json.RawMessage(`{"role":"user","content":"hi"}`),
		},
		{
			Type: "assistant", UUID: "a1", ParentUUID: &parent, SessionID: sid,
			Timestamp: Now(), CWD: cwd, UserType: "external", Version: "test",
			RequestID: "req_1",
			Message:   json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"hello"}]}`),
		},
	}
	for _, e := range entries {
		if err := w.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := Read(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("read %d entries, want 2", len(got))
	}
	if got[0].Type != "user" || got[1].Type != "assistant" {
		t.Errorf("types = %s,%s", got[0].Type, got[1].Type)
	}
	if got[1].ParentUUID == nil || *got[1].ParentUUID != "u1" {
		t.Errorf("parentUuid not preserved: %v", got[1].ParentUUID)
	}
}

func TestMostRecent(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/proj"
	if _, ok := MostRecent(cwd); ok {
		t.Error("expected no session initially")
	}
	w, err := NewWriter(cwd, "only-session")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(Entry{Type: "user", Message: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if id, ok := MostRecent(cwd); !ok || id != "only-session" {
		t.Errorf("MostRecent = %q,%v", id, ok)
	}
}

func TestMostRecentIncludesLegacyProjectsRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", root)
	cwd := "/work/proj"
	legacy := filepath.Join(root, "projects", EncodePath(cwd), "legacy-session.jsonl")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if id, ok := MostRecent(cwd); !ok || id != "legacy-session" {
		t.Fatalf("MostRecent = %q,%v; want legacy-session,true", id, ok)
	}
}
