package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPromptHistoryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p", promptHistoryFile)
	h := NewPromptHistory(path, 10)
	if got, err := h.Load(); err != nil || len(got) != 0 {
		t.Fatalf("missing file: Load = %v, %v; want empty, nil", got, err)
	}
	for _, e := range []string{"first", "two\nlines", "two\nlines", "third"} {
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewPromptHistory(path, 10).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first", "two\nlines", "third"} // adjacent duplicate dropped
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %q, want %q", got, want)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("history file mode = %o, want 600", perm)
	}
}

func TestPromptHistoryCapsAndTrims(t *testing.T) {
	path := filepath.Join(t.TempDir(), promptHistoryFile)
	h := NewPromptHistory(path, 3)
	for _, e := range []string{"a", "b", "c", "d", "e"} {
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NewPromptHistory(path, 3).Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"c", "d", "e"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %q, want %q", got, want)
	}
	// Loading an over-long file rewrites it to the cap.
	raw, _ := os.ReadFile(path)
	if n := strings.Count(string(raw), "\n"); n != 3 {
		t.Errorf("file has %d lines after trim, want 3:\n%s", n, raw)
	}
}

func TestPromptHistorySkipsUnreadableLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), promptHistoryFile)
	if err := os.WriteFile(path, []byte("\"ok\"\n{not json\n\"also ok\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := NewPromptHistory(path, 10).Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ok", "also ok"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Load = %q, want %q", got, want)
	}
}

// The history file sits beside the transcripts. It must never be taken for
// one, or --continue would try to resume it.
func TestPromptHistoryIsNotASession(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	cwd := "/work/project"
	if err := NewPromptHistory(PromptHistoryPath(cwd), 10).Append("hello"); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(PromptHistoryPath(cwd)) != Dir(cwd) {
		t.Fatalf("history path %s is not in the project's sessions dir %s", PromptHistoryPath(cwd), Dir(cwd))
	}
	if id, ok := MostRecent(cwd); ok {
		t.Fatalf("MostRecent picked %q from a dir holding only the history file", id)
	}
}
