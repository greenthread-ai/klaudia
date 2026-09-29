package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func runTool(t *testing.T, tool interface {
	Execute(context.Context, Context, json.RawMessage) ([]Result, error)
}, dir string, input map[string]any) Result {
	t.Helper()
	raw, _ := json.Marshal(input)
	res, err := tool.Execute(context.Background(), Context{WorkingDir: dir}, raw)
	if err != nil {
		t.Fatal(err)
	}
	return res[0]
}

// Write replaced a CRLF file's line endings with the model's LF.
func TestWriteKeepsCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "win.txt")
	os.WriteFile(path, []byte("one\r\ntwo\r\n"), 0o644)
	w, _ := NewWrite()
	if r := runTool(t, w, dir, map[string]any{"file_path": path, "content": "one\ntwo\nthree\n"}); r.IsError {
		t.Fatal(r.Content)
	}
	if got, _ := os.ReadFile(path); string(got) != "one\r\ntwo\r\nthree\r\n" {
		t.Errorf("content = %q, want CRLF kept", got)
	}
	// An LF file, and a new file, are written as given.
	lf := filepath.Join(dir, "unix.txt")
	os.WriteFile(lf, []byte("a\n"), 0o644)
	runTool(t, w, dir, map[string]any{"file_path": lf, "content": "b\n"})
	if got, _ := os.ReadFile(lf); string(got) != "b\n" {
		t.Errorf("LF file = %q", got)
	}
}

// Writes go through a temporary file and a rename: the mode is kept, a
// symlink is written through to its target, and nothing is left behind.
func TestWritesAreAtomic(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	os.WriteFile(script, []byte("echo old\n"), 0o644)
	os.Chmod(script, 0o755) // explicit: the umask may have narrowed WriteFile's mode
	link := filepath.Join(dir, "link.sh")
	os.Symlink(script, link)

	e, _ := NewEdit()
	if r := runTool(t, e, dir, map[string]any{"file_path": link, "old_string": "old", "new_string": "new"}); r.IsError {
		t.Fatal(r.Content)
	}
	if got, _ := os.ReadFile(script); string(got) != "echo new\n" {
		t.Errorf("target = %q, want the edit written through the link", got)
	}
	if st, _ := os.Lstat(link); st.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file")
	}
	if st, _ := os.Stat(script); st.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755 kept", st.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("directory has %d entries, want no temporary files left", len(entries))
	}

	// A failed write leaves the original untouched and no temporary file.
	ro := t.TempDir()
	orig := filepath.Join(ro, "keep.txt")
	os.WriteFile(orig, []byte("intact"), 0o644)
	os.Chmod(ro, 0o555)
	defer os.Chmod(ro, 0o755)
	if err := writeFileAtomic(orig, []byte("new"), 0o644); err == nil && os.Geteuid() != 0 {
		t.Error("writing into a read-only directory succeeded")
	}
	if got, _ := os.ReadFile(orig); string(got) != "intact" {
		t.Errorf("original = %q after a failed write, want intact", got)
	}
}

// A new file is created as before, so the umask still applies to it.
func TestNewFileRespectsUmask(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	if err := writeFileAtomic(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(dir, "ref.txt")
	os.WriteFile(ref, []byte("x"), 0o644)
	a, _ := os.Stat(path)
	b, _ := os.Stat(ref)
	if a.Mode().Perm() != b.Mode().Perm() {
		t.Errorf("new file mode %v, os.WriteFile gives %v under the same umask", a.Mode().Perm(), b.Mode().Perm())
	}
}
