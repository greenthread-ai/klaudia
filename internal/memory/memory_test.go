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

func TestAddLinksMemoryFiles(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "tools.md"), []byte("# Tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "prefs.md"), []byte("# Prefs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte("# Legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.Add("remember this"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	for _, want := range []string{"## Linked memory", "[prefs](memory/prefs.md)", "[tools](memory/tools.md)"} {
		if !strings.Contains(contents, want) {
			t.Fatalf("Index() = %q, want %q", contents, want)
		}
	}
	if strings.Contains(contents, "memory/MEMORY.md") {
		t.Fatalf("Index() = %q, should not link legacy MEMORY.md", contents)
	}
}

func TestAddDoesNotDuplicateMemoryFileLinks(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "tools.md"), []byte("# Tools\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	addNotes(t, store, "first", "second")

	contents, err := store.Index()
	if err != nil {
		t.Fatalf("Index() error = %v", err)
	}
	if got := strings.Count(contents, "[tools](memory/tools.md)"); got != 1 {
		t.Fatalf("tools link count = %d, want 1; contents = %q", got, contents)
	}
}

func addNotes(t *testing.T, store *Store, notes ...string) {
	t.Helper()
	for _, note := range notes {
		if err := store.Add(note); err != nil {
			t.Fatalf("Add(%q) error = %v", note, err)
		}
	}
}
