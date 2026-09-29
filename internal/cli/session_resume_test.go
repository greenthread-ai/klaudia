package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/session"
)

// fakeChat is an OpenAI-compatible endpoint that answers every request with a
// fixed text reply and keeps each request's raw body, so a test can see what
// history the model was actually sent.
type fakeChat struct {
	mu     sync.Mutex
	bodies []string
}

func (f *fakeChat) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.bodies = append(f.bodies, string(b))
	n := len(f.bodies)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"reply-%d\"},\"finish_reason\":\"stop\"}]}\n\n", n)
	fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func (f *fakeChat) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return ""
	}
	return f.bodies[len(f.bodies)-1]
}

// workdir makes a project dir whose local config points Klaudia at the fake.
func workdir(t *testing.T, baseURL string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := fmt.Sprintf("provider = \"openai\"\nbaseURL = %q\napiKey = \"test\"\nmodel = \"fake-model\"\n", baseURL)
	if err := os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".klaudia", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// embed runs one stream-json embedding launch in cwd, sending prompts as user
// lines, and returns the output lines.
func embed(t *testing.T, cwd string, args []string, prompts ...string) ([]map[string]any, error) {
	t.Helper()
	t.Chdir(cwd)
	var in bytes.Buffer
	for _, p := range prompts {
		line, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": p}})
		in.Write(append(line, '\n'))
	}
	var out bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetArgs(append([]string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--permission-mode", "dontAsk"}, args...))
	cmd.SetIn(&in)
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	err := cmd.ExecuteContext(context.Background())
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil {
			lines = append(lines, m)
		}
	}
	return lines, err
}

// TestEmbeddingSessionIDResumesFromAnotherDirAndHost is the embedder's
// lifecycle: pin the id on the first launch in one working dir, stop, carry
// the sessions root to a fresh config dir (another host), and resume by id from
// a different working dir — with the model seeing the earlier conversation and
// the transcript continuing in one file.
func TestEmbeddingSessionIDResumesFromAnotherDirAndHost(t *testing.T) {
	fake := &fakeChat{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	hostA := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", hostA)
	dirA := workdir(t, srv.URL)

	lines, err := embed(t, dirA, []string{"--session-id", "chat-42"}, "remember the word pineapple")
	if err != nil {
		t.Fatalf("first launch: %v", err)
	}
	if lines[0]["type"] != "system" || lines[0]["subtype"] != "init" || lines[0]["session_id"] != "chat-42" || lines[0]["resumed"] != false {
		t.Fatalf("first line = %v, want a fresh system/init for chat-42", lines[0])
	}
	transcriptA := filepath.Join(hostA, "sessions", session.EncodePath(dirA), "chat-42.jsonl")
	if _, err := os.Stat(transcriptA); err != nil {
		t.Fatalf("pinned transcript not written: %v", err)
	}

	// "Another host": copy the sessions root under a new config dir, drop the
	// original, and resume from a different working dir.
	hostB := t.TempDir()
	if err := os.CopyFS(filepath.Join(hostB, "sessions"), os.DirFS(filepath.Join(hostA, "sessions"))); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(hostA); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KLAUDIA_CONFIG_DIR", hostB)
	dirB := workdir(t, srv.URL)

	lines, err = embed(t, dirB, []string{"--resume", "chat-42"}, "what was the word?")
	if err != nil {
		t.Fatalf("resume launch: %v", err)
	}
	init := lines[0]
	if init["type"] != "system" || init["session_id"] != "chat-42" || init["resumed"] != true || init["history_messages"] != float64(2) {
		t.Fatalf("resume init = %v, want chat-42 resumed with 2 history messages", init)
	}
	if body := fake.last(); !strings.Contains(body, "pineapple") || !strings.Contains(body, "reply-1") {
		t.Fatalf("resumed turn did not carry the earlier conversation to the model:\n%s", body)
	}
	moved := filepath.Join(hostB, "sessions", session.EncodePath(dirA), "chat-42.jsonl")
	entries, err := session.Read(moved)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("transcript has %d entries, want 4 (both turns in the one file)", len(entries))
	}
	if _, err := os.Stat(filepath.Join(hostB, "sessions", session.EncodePath(dirB), "chat-42.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resume from dirB split the transcript into dirB's project dir (stat err %v)", err)
	}

	// The id is taken now: pinning it again without --resume is a usage error.
	if _, err := embed(t, dirB, []string{"--session-id", "chat-42"}, "hi"); !isUsageError(err) {
		t.Fatalf("--session-id of an existing session: err = %v, want a usage error", err)
	}
}

func TestSessionIDRejectsUnsafeIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	dir := workdir(t, "http://127.0.0.1:1")
	for _, args := range [][]string{
		{"--session-id", "../escape"},
		{"--session-id", "a/b"},
		{"--resume", "../../etc/passwd"},
	} {
		if _, err := embed(t, dir, args, "hi"); !isUsageError(err) {
			t.Errorf("%v: err = %v, want a usage error", args, err)
		}
	}
}

func TestChooseSessionID(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/a"
	seedSession(t, cwd, "old")

	cases := []struct {
		name     string
		opts     options
		resumeID string
		wantID   string // "" = minted
		wantPath bool
		wantErr  bool
	}{
		{name: "fresh", wantID: ""},
		{name: "pinned", opts: options{sessionID: "new"}, wantID: "new"},
		{name: "pinned exists", opts: options{sessionID: "old"}, wantErr: true},
		{name: "resume", opts: options{resume: "old"}, resumeID: "old", wantID: "old", wantPath: true},
		{name: "resume same id", opts: options{resume: "old", sessionID: "old"}, resumeID: "old", wantID: "old", wantPath: true},
		{name: "resume into new id forks", opts: options{resume: "old", sessionID: "new"}, resumeID: "old", wantID: "new"},
		{name: "fork-session mints", opts: options{resume: "old", forkSession: true}, resumeID: "old", wantID: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, path, err := chooseSessionID(cwd, tc.opts, tc.resumeID)
			if tc.wantErr {
				if !isUsageError(err) {
					t.Fatalf("err = %v, want a usage error", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantID != "" && id != tc.wantID {
				t.Errorf("id = %q, want %q", id, tc.wantID)
			}
			if tc.wantID == "" && (id == "" || id == "old") {
				t.Errorf("id = %q, want a freshly minted id", id)
			}
			if (path != "") != tc.wantPath {
				t.Errorf("path = %q, wantPath %v", path, tc.wantPath)
			}
		})
	}
}

func isUsageError(err error) bool { return err != nil && exitCodeFor(err) == ExitUsage }
