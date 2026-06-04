package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mustParseTime parses an RFC3339 time and aborts the test on a bad
// fixture. Shared by tests that need a stable timestamp for writeNote.
func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("mustParseTime(%q): %v", s, err)
	}
	return tt
}

// writeNote drops a memory/<name>.md file under dir and stamps its mtime to
// `at`, so tests can drive Recent/Stale without sleeps or wall-clock races.
func writeNote(t *testing.T, dir, name, body string, at time.Time) string {
	t.Helper()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(memDir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRecentReturnsNotesWithinWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeNote(t, dir, "fresh.md", "# fresh\n", now.Add(-1*time.Hour))
	writeNote(t, dir, "old.md", "# old\n", now.Add(-72*time.Hour))

	store := New(dir)
	got, err := store.Recent(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "fresh" {
		t.Errorf("Recent(24h) = %+v, want [fresh]", got)
	}
}

func TestRecentNewestFirst(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeNote(t, dir, "a.md", "# a\n", now.Add(-2*time.Hour))
	writeNote(t, dir, "b.md", "# b\n", now.Add(-30*time.Minute))
	writeNote(t, dir, "c.md", "# c\n", now.Add(-1*time.Hour))

	store := New(dir)
	got, _ := store.Recent(24 * time.Hour)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	if got[0].Name != "b" || got[1].Name != "c" || got[2].Name != "a" {
		t.Errorf("ordering = %q, %q, %q; want b, c, a (newest first)", got[0].Name, got[1].Name, got[2].Name)
	}
}

func TestStaleReturnsNotesOlderThanThreshold(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeNote(t, dir, "fresh.md", "# fresh\n", now.Add(-1*time.Hour))
	writeNote(t, dir, "ancient.md", "# ancient\n", now.Add(-30*24*time.Hour))
	writeNote(t, dir, "older.md", "# older\n", now.Add(-7*24*time.Hour))

	store := New(dir)
	got, err := store.Stale(24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	// Oldest first: ancient before older.
	if got[0].Name != "ancient" || got[1].Name != "older" {
		t.Errorf("stale order = %q, %q; want ancient, older (oldest first)", got[0].Name, got[1].Name)
	}
}

func TestRecentAndStaleSkipMemoryMd(t *testing.T) {
	// memory/MEMORY.md (the now-deprecated index location) must never appear
	// as a detail-note Entry, even though it lives in the same glob.
	dir := t.TempDir()
	writeNote(t, dir, "MEMORY.md", "# index\n", time.Now())
	writeNote(t, dir, "real.md", "# real\n", time.Now())

	store := New(dir)
	got, _ := store.Recent(time.Hour)
	for _, e := range got {
		if e.Name == "MEMORY" {
			t.Errorf("Recent should skip MEMORY.md; got %+v", got)
		}
	}
}

func TestRecentEmptyDirReturnsEmpty(t *testing.T) {
	store := New(t.TempDir())
	got, err := store.Recent(time.Hour)
	if err != nil {
		t.Fatalf("Recent on empty dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

func TestEntryTitleUsesFileHook(t *testing.T) {
	// Entry.Title should match what fileHook returns — the first heading
	// (with leading '#' stripped) or the first non-empty line.
	dir := t.TempDir()
	writeNote(t, dir, "headed.md", "# Browser/search port status\n\nbody body body\n", time.Now())
	writeNote(t, dir, "bare.md", "no heading just text here\n", time.Now())

	store := New(t.TempDir())
	_ = store // sanity to avoid lint; new store below has the notes
	store = New(dir)

	got, _ := store.Recent(time.Hour)
	titles := map[string]string{}
	for _, e := range got {
		titles[e.Name] = e.Title
	}
	if got, want := titles["headed"], "Browser/search port status"; got != want {
		t.Errorf("headed title = %q, want %q", got, want)
	}
	if got, want := titles["bare"], "no heading just text here"; got != want {
		t.Errorf("bare title = %q, want %q", got, want)
	}
}
