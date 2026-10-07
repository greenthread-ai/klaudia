package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func writeDetailNote(t *testing.T, dir, name, content string) string {
	t.Helper()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(memDir, name+".md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A note with frontmatter used to get the hook "---", its first line (#121).
func TestNoteHookSkipsFrontmatter(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"description wins", "---\ndescription: How we cut releases\ntags: [ops]\n---\n# Release\n", "How we cut releases"},
		{"multi-line description is one line", "---\ndescription: >-\n  first half\n  second half\n---\nbody\n", "first half second half"},
		{"no description uses the body", "---\ntags: [ops]\nstatus: active\n---\n\n# Release checklist\n", "Release checklist"},
		{"unparseable yaml is skipped", "---\ntags: [unclosed\n---\n# Heading after bad yaml\n", "Heading after bad yaml"},
		{"crlf fences are skipped", "---\r\ntags: [a]\r\n---\r\n# Windows note\r\n", "Windows note"},
		{"unclosed fence skips the fence line", "---\n# Heading\n", "Heading"},
		{"plain note", "\n## Tools\nuse doublestar\n", "Tools"},
		{"bare hash line is skipped", "#\nreal text\n", "real text"},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := noteHook([]byte(c.content)); got != c.want {
			t.Errorf("%s: noteHook = %q, want %q", c.name, got, c.want)
		}
	}
}

// The hook was cut at byte 80, which can land inside a multi-byte rune.
func TestNoteHookTruncatesOnRuneBoundary(t *testing.T) {
	line := strings.Repeat("日本語", 40) // 3-byte runes: byte 80 falls mid-rune
	got := noteHook([]byte("# " + line + "\n"))
	if !utf8.ValidString(got) {
		t.Fatalf("hook is not valid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n != hookMaxRunes+1 {
		t.Errorf("hook has %d runes, want %d plus the ellipsis", n, hookMaxRunes)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("hook %q should end with an ellipsis", got)
	}
	if short := noteHook([]byte("# exactly short\n")); short != "exactly short" {
		t.Errorf("short hook = %q, want it untouched", short)
	}
}

// Promote adds frontmatter to a note that had none; its index pointer must
// keep the note's heading rather than become "---".
func TestFilePointersAfterPromoteKeepHeading(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	writeDetailNote(t, dir, "release", "# Release checklist\n\nTag, then push.\n")
	if err := store.Promote("release"); err != nil {
		t.Fatal(err)
	}
	ptrs := store.FilePointers()
	want := "- [release](memory/release.md) — Release checklist"
	if len(ptrs) != 1 || ptrs[0] != want {
		t.Fatalf("FilePointers = %q, want [%q]", ptrs, want)
	}
}

// A description in frontmatter survives the Promote rewrite.
func TestPromoteKeepsDescription(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	path := writeDetailNote(t, dir, "db", "---\ndescription: Postgres gotchas\n---\nuse pgx\n")
	if err := store.Promote("db"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "description: Postgres gotchas") {
		t.Errorf("Promote dropped the description:\n%s", data)
	}
	if hook := fileHook(path); hook != "Postgres gotchas" {
		t.Errorf("hook after Promote = %q", hook)
	}
}

func TestRemoveDeletesOneSessionNote(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	// Notes before the detail note: on main a second Add after the linked
	// section exists is lost (#97, fixed separately).
	addNotes(t, store, "use doublestar for globs", "the api key lives in vault")
	writeDetailNote(t, dir, "tools", "# Tools\n")
	if err := store.SyncLinks(); err != nil {
		t.Fatal(err)
	}

	removed, err := store.Remove("DOUBLESTAR")
	if err != nil {
		t.Fatalf("Remove err = %v", err)
	}
	if len(removed) != 1 || !strings.HasSuffix(removed[0], "use doublestar for globs") {
		t.Fatalf("Remove = %q", removed)
	}
	idx, _ := store.Index()
	if strings.Contains(idx, "doublestar") {
		t.Errorf("index still holds the removed note:\n%s", idx)
	}
	for _, keep := range []string{"# Memory", "the api key lives in vault", linkedSectionHeader, "- [tools](memory/tools.md) — Tools"} {
		if !strings.Contains(idx, keep) {
			t.Errorf("index lost %q:\n%s", keep, idx)
		}
	}
}

func TestRemoveRefusesAmbiguousQuery(t *testing.T) {
	store := New(t.TempDir())
	addNotes(t, store, "deploy with make release", "deploy needs the vpn")
	before, _ := store.Index()

	matched, err := store.Remove("deploy")
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("Remove err = %v, want ErrAmbiguous", err)
	}
	if len(matched) != 2 {
		t.Errorf("candidates = %q, want both notes", matched)
	}
	if after, _ := store.Index(); after != before {
		t.Errorf("an ambiguous Remove changed the index:\n%s", after)
	}
	// Narrowed, it removes exactly one.
	if _, err := store.Remove("deploy vpn"); err != nil {
		t.Fatalf("narrowed Remove err = %v", err)
	}
	entries, _ := store.Entries()
	if len(entries) != 1 || !strings.Contains(entries[0], "make release") {
		t.Errorf("entries after Remove = %q", entries)
	}
}

func TestRemoveNeverMatchesLinkedPointers(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	writeDetailNote(t, dir, "tools", "# Tools\n")
	addNotes(t, store, "unrelated")
	if err := store.SyncLinks(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Remove("tools"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove(pointer text) err = %v, want ErrNotFound", err)
	}
	if _, err := store.Remove("  "); !errors.Is(err, ErrEmpty) {
		t.Errorf("Remove(blank) err = %v, want ErrEmpty", err)
	}
	if _, err := New(t.TempDir()).Remove("x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove on a store without MEMORY.md err = %v, want ErrNotFound", err)
	}
}

func TestRemoveNoteDeletesFileAndPointer(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	path := writeDetailNote(t, dir, "tools", "# Tools\n")
	writeDetailNote(t, dir, "db", "# DB\n")
	addNotes(t, store, "keep me")

	if err := store.RemoveNote("tools.md"); err != nil {
		t.Fatalf("RemoveNote err = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("note file still exists: %v", err)
	}
	idx, _ := store.Index()
	if strings.Contains(idx, "memory/tools.md") || !strings.Contains(idx, "memory/db.md") || !strings.Contains(idx, "keep me") {
		t.Errorf("index after RemoveNote:\n%s", idx)
	}

	if err := store.RemoveNote("tools"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RemoveNote(missing) err = %v, want ErrNotFound", err)
	}
	for _, bad := range []string{"", "..", "../MEMORY", "a/b", `a\b`, "MEMORY"} {
		if err := store.RemoveNote(bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("RemoveNote(%q) err = %v, want ErrInvalidName", bad, err)
		}
	}
}
