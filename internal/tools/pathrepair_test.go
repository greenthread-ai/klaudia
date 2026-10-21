package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The macOS screenshot case, byte for byte: the name holds U+202F (e2 80 af)
// before "pm" and every retyping of it uses a plain space.
const nnbsp = "\u202f"

func TestReadRepairsUnicodeSpaces(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "Screenshot 2026-10-21 at 4.44.59"+nnbsp+"pm.txt")
	if err := os.WriteFile(real, []byte("pixels\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	asked := filepath.Join(dir, "Screenshot 2026-10-21 at 4.44.59 pm.txt")
	if _, err := os.Lstat(asked); err == nil {
		t.Fatal("test is meaningless: the plain-space path exists")
	}

	read, err := NewRead()
	if err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(map[string]any{"file_path": asked})
	res, err := read.Execute(context.Background(), Context{WorkingDir: dir}, in)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].IsError || !strings.Contains(res[0].Content, "pixels") {
		t.Fatalf("Read did not reach the file: %+v", res[0])
	}
}

func TestEditRepairsUnicodeSpaces(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "My"+"\u00a0"+"Notes.txt")
	if err := os.WriteFile(real, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	edit, err := NewEdit()
	if err != nil {
		t.Fatal(err)
	}
	in, _ := json.Marshal(map[string]any{
		"file_path":  filepath.Join(dir, "My Notes.txt"),
		"old_string": "before", "new_string": "after",
	})
	res, err := edit.Execute(context.Background(), Context{WorkingDir: dir}, in)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].IsError {
		t.Fatalf("Edit did not reach the file: %+v", res[0])
	}
	got, _ := os.ReadFile(real)
	if string(got) != "after\n" {
		t.Errorf("file = %q, want %q", got, "after\n")
	}
}

// A mismatch in a DIRECTORY component is repaired too — "~/My Documents/x" with
// the wrong space is the same failure one level up.
func TestRepairWalksDirectoryComponents(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "My"+nnbsp+"Documents")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "a b.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	asked := filepath.Join(root, "My Documents", "a b.txt")
	got := resolveExistingPath(Context{WorkingDir: root}, asked)
	if _, err := os.Lstat(got); err != nil {
		t.Fatalf("resolveExistingPath(%q) = %q, which does not exist", asked, got)
	}
}

// Two candidates differing only in whitespace means we cannot know which was
// meant. The ENOENT is the honest answer; picking one would read the wrong file.
func TestRepairRefusesAmbiguity(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a b.txt", "a" + nnbsp + "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	asked := filepath.Join(dir, "a"+"\u00a0"+"b.txt")
	if got := resolveExistingPath(Context{WorkingDir: dir}, asked); got != asked {
		t.Errorf("resolveExistingPath = %q, want the untouched %q", got, asked)
	}
}

// Repair must never turn one real file into another. Names that differ by
// anything but whitespace are left alone.
func TestRepairOnlyNormalisesSpaces(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "report-final.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	asked := filepath.Join(dir, "report final.txt")
	if got := resolveExistingPath(Context{WorkingDir: dir}, asked); got != asked {
		t.Errorf("resolveExistingPath = %q, want the untouched %q", got, asked)
	}
}

// Write creates files. A new name that is space-equivalent to an existing one
// is a new file, not a request to overwrite that one.
func TestWriteDoesNotRepair(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "notes"+nnbsp+"v2.txt")
	if err := os.WriteFile(existing, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := NewWrite()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "notes v2.txt")
	in, _ := json.Marshal(map[string]any{"file_path": want, "content": "new\n"})
	if _, err := w.Execute(context.Background(), Context{WorkingDir: dir}, in); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(existing); string(got) != "original\n" {
		t.Errorf("Write clobbered the space-equivalent file: %q", got)
	}
	if got, _ := os.ReadFile(want); string(got) != "new\n" {
		t.Errorf("Write did not create the requested path: %q", got)
	}
}
