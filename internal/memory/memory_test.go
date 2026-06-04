package memory

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestIndexMissingFileReturnsEmpty(t *testing.T) {
	store := New(t.TempDir())

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if contents != "" {
		t.Fatalf("Index() = %q, want empty string", contents)
	}
}

func TestAddThenIndexContainsTextAndHeader(t *testing.T) {
	store := New(t.TempDir())

	if err := store.Add("remember this"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if !strings.Contains(contents, "# Memory\n\n") {
		t.Fatalf("Index() = %q, want header", contents)
	}
	if !strings.Contains(contents, "remember this") {
		t.Fatalf("Index() = %q, want added text", contents)
	}
}

func TestTwoAddsProduceTwoBulletLines(t *testing.T) {
	store := New(t.TempDir())

	if err := store.Add("first"); err != nil {
		t.Fatalf("Add() first error = %v", err)
	}
	if err := store.Add("second"); err != nil {
		t.Fatalf("Add() second error = %v", err)
	}

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(contents), "\n")
	bulletLines := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "- ") {
			bulletLines++
		}
	}
	if bulletLines != 2 {
		t.Fatalf("bullet lines = %d, want 2; contents = %q", bulletLines, contents)
	}
}

func TestAddEmptyStringReturnsError(t *testing.T) {
	store := New(t.TempDir())

	if err := store.Add(" \t\n "); err == nil {
		t.Fatal("Add() error = nil, want error")
	}
}

func TestEntriesMissingFileReturnsEmpty(t *testing.T) {
	store := New(t.TempDir())

	entries, err := store.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("Entries() = %q, want empty slice", entries)
	}
}

func TestEntriesAfterAddingThreeNotesReturnsThree(t *testing.T) {
	store := New(t.TempDir())
	addNotes(t, store, "first", "second", "third")

	entries, err := store.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len(Entries()) = %d, want 3; entries = %q", len(entries), entries)
	}
	for i, want := range []string{"first", "second", "third"} {
		if !strings.Contains(entries[i], want) {
			t.Fatalf("Entries()[%d] = %q, want to contain %q", i, entries[i], want)
		}
		if strings.HasPrefix(entries[i], "- ") {
			t.Fatalf("Entries()[%d] = %q, want bullet prefix stripped", i, entries[i])
		}
	}
}

func TestSearchMatchesCaseInsensitively(t *testing.T) {
	store := New(t.TempDir())
	addNotes(t, store, "Alpha Note", "beta note", "gamma")

	matches, err := store.Search("alpha")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "Alpha Note") {
		t.Fatalf("Search() = %q, want Alpha Note", matches)
	}
}

func TestSearchMultipleTermsRequiresAllTerms(t *testing.T) {
	store := New(t.TempDir())
	addNotes(t, store, "alpha beta", "alpha gamma", "beta gamma")

	matches, err := store.Search("alpha beta")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(matches) != 1 || !strings.Contains(matches[0], "alpha beta") {
		t.Fatalf("Search() = %q, want only alpha beta", matches)
	}
}

func TestSearchEmptyQueryReturnsAll(t *testing.T) {
	store := New(t.TempDir())
	addNotes(t, store, "first", "second", "third")

	entries, err := store.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	matches, err := store.Search(" \t\n ")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if !reflect.DeepEqual(matches, entries) {
		t.Fatalf("Search(empty) = %q, want %q", matches, entries)
	}
}

func TestFilePointers(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A file whose hook comes from its first heading.
	if err := os.WriteFile(filepath.Join(memDir, "tools.md"), []byte("# Preferred tools\n\n- rg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file with no heading: hook is the first non-empty line.
	if err := os.WriteFile(filepath.Join(memDir, "prefs.md"), []byte("\nlikes tabs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A nested index file must be ignored.
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := store.FilePointers()
	want := []string{
		"- [prefs](memory/prefs.md) — likes tabs",
		"- [tools](memory/tools.md) — Preferred tools",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilePointers() = %#v, want %#v", got, want)
	}
}

func TestSyncLinksWritesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "tools.md"), []byte("# Preferred tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed an index with a session bullet.
	if err := store.Add("prefer table tests"); err != nil { // Add also syncs
		t.Fatal(err)
	}

	contents, err := store.Index()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"prefer table tests", "## Linked memory", "- [tools](memory/tools.md) — Preferred tools"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("Index() = %q, missing %q", contents, want)
		}
	}

	// Re-syncing without changes must not rewrite the file (idempotent) and must
	// not duplicate the section.
	before, _ := os.ReadFile(store.Path())
	infoBefore, _ := os.Stat(store.Path())
	if err := store.SyncLinks(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(store.Path())
	if string(before) != string(after) {
		t.Fatalf("SyncLinks rewrote an unchanged file:\nbefore=%q\nafter=%q", before, after)
	}
	if infoAfter, _ := os.Stat(store.Path()); !infoAfter.ModTime().Equal(infoBefore.ModTime()) {
		t.Error("SyncLinks touched the file despite no change")
	}
	if got := strings.Count(string(after), linkedSectionHeader); got != 1 {
		t.Fatalf("linked section appears %d times, want 1", got)
	}

	// Removing the note strips the section on the next sync.
	if err := os.Remove(filepath.Join(memDir, "tools.md")); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncLinks(); err != nil {
		t.Fatal(err)
	}
	final, _ := os.ReadFile(store.Path())
	if strings.Contains(string(final), linkedSectionHeader) {
		t.Fatalf("stale linked section survived removal: %q", final)
	}
	if !strings.Contains(string(final), "prefer table tests") {
		t.Fatalf("session bullets must survive link sync: %q", final)
	}
}

func TestSearchIncludesDetailFiles(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	if err := store.Add("prefer table tests"); err != nil {
		t.Fatal(err)
	}
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "browser.md"),
		[]byte("# Browser port\n\n- DDG search hits an anomaly modal in headless Chrome\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// A term only present in the detail file is found, tagged with the filename.
	matches, err := store.Search("anomaly")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(matches) != 1 || !strings.HasPrefix(matches[0], "browser.md: ") || !strings.Contains(matches[0], "anomaly modal") {
		t.Fatalf("Search(anomaly) = %#v, want one browser.md-tagged hit", matches)
	}

	// Headings are not matchable content lines.
	if got, _ := store.Search("Browser port"); len(got) != 0 {
		t.Fatalf("Search should skip headings, got %#v", got)
	}
}

func addNotes(t *testing.T, store Store, notes ...string) {
	t.Helper()
	for _, note := range notes {
		if err := store.Add(note); err != nil {
			t.Fatalf("Add(%q) error = %v", note, err)
		}
	}
}
