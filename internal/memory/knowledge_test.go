package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFSKnowledgeReadMissingReturnsEmpty(t *testing.T) {
	k := NewKnowledge(t.TempDir())
	got, err := k.Read()
	if err != nil {
		t.Fatalf("Read on missing knowledge: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestFSKnowledgeAddCreatesHeaderAndBullet(t *testing.T) {
	dir := t.TempDir()
	k := NewKnowledge(dir)
	if err := k.Add("first lesson"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, err := os.ReadFile(k.Path())
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "# Project Knowledge\n\n") {
		t.Errorf("missing header: %q", got)
	}
	if !strings.Contains(got, "first lesson") {
		t.Errorf("missing body: %q", got)
	}
}

func TestFSKnowledgeAddRejectsEmpty(t *testing.T) {
	k := NewKnowledge(t.TempDir())
	if err := k.Add("   \n  "); !errors.Is(err, ErrEmpty) {
		t.Errorf("got %v, want ErrEmpty", err)
	}
}

func TestPromoteCopiesBodyAndMarksSuperseded(t *testing.T) {
	dir := t.TempDir()
	notePath := writeNote(t, dir, "decision.md",
		"---\ntags: [decision]\n---\n\n# Use Postgres\n\nWe chose pg for the reasons in this note.\n",
		mustParseTime(t, "2026-06-01T12:00:00Z"))

	store := New(dir)
	if err := store.Promote("decision"); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	// Body landed in KNOWLEDGE.md (with the trim + timestamp from appendBullet).
	kdata, err := os.ReadFile(filepath.Join(dir, "KNOWLEDGE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kdata), "Use Postgres") || !strings.Contains(string(kdata), "We chose pg") {
		t.Errorf("KNOWLEDGE.md missing promoted body: %q", kdata)
	}

	// Source still exists, with status: superseded in its rewritten frontmatter.
	src, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatal(err)
	}
	meta, body := parseFrontmatter(src)
	if meta.Status != "superseded" {
		t.Errorf("source status = %q, want superseded", meta.Status)
	}
	if meta.SupersededBy != "KNOWLEDGE.md" {
		t.Errorf("source superseded_by = %q, want KNOWLEDGE.md", meta.SupersededBy)
	}
	if !strings.Contains(string(body), "Use Postgres") {
		t.Errorf("source body should be intact (audit trail); got %q", body)
	}
}

func TestPromoteUnknownNoteReturnsErrNotFound(t *testing.T) {
	store := New(t.TempDir())
	if err := store.Promote("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestPromoteEmptyBodyReturnsErrEmpty(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "empty.md", "---\ntags: [x]\n---\n\n", mustParseTime(t, "2026-01-01T00:00:00Z"))

	store := New(dir)
	if err := store.Promote("empty"); !errors.Is(err, ErrEmpty) {
		t.Errorf("got %v, want ErrEmpty", err)
	}
}

func TestSupersedeRewritesBothFrontmatters(t *testing.T) {
	dir := t.TempDir()
	oldPath := writeNote(t, dir, "old.md", "# Old\n\nthe old way\n", mustParseTime(t, "2026-01-01T00:00:00Z"))
	newPath := writeNote(t, dir, "new.md", "# New\n\nthe new way\n", mustParseTime(t, "2026-06-01T00:00:00Z"))

	store := New(dir)
	if err := store.Supersede("old", "new"); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	oldData, _ := os.ReadFile(oldPath)
	oldMeta, _ := parseFrontmatter(oldData)
	if oldMeta.Status != "superseded" || oldMeta.SupersededBy != "new" {
		t.Errorf("old frontmatter = %+v, want status=superseded superseded_by=new", oldMeta)
	}

	newData, _ := os.ReadFile(newPath)
	newMeta, _ := parseFrontmatter(newData)
	if newMeta.Supersedes != "old" {
		t.Errorf("new.supersedes = %q, want old", newMeta.Supersedes)
	}
}

func TestSupersedeIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "old.md", "# Old\n", mustParseTime(t, "2026-01-01T00:00:00Z"))
	writeNote(t, dir, "new.md", "# New\n", mustParseTime(t, "2026-06-01T00:00:00Z"))

	store := New(dir)
	if err := store.Supersede("old", "new"); err != nil {
		t.Fatalf("first Supersede: %v", err)
	}
	// Capture state after first call.
	firstOld, _ := os.ReadFile(filepath.Join(dir, "memory", "old.md"))
	firstNew, _ := os.ReadFile(filepath.Join(dir, "memory", "new.md"))

	if err := store.Supersede("old", "new"); err != nil {
		t.Fatalf("second Supersede: %v", err)
	}
	secondOld, _ := os.ReadFile(filepath.Join(dir, "memory", "old.md"))
	secondNew, _ := os.ReadFile(filepath.Join(dir, "memory", "new.md"))

	if string(firstOld) != string(secondOld) {
		t.Errorf("old.md changed between idempotent calls")
	}
	if string(firstNew) != string(secondNew) {
		t.Errorf("new.md changed between idempotent calls")
	}
}

func TestSupersedeUnknownNotesReturnErrNotFound(t *testing.T) {
	dir := t.TempDir()
	writeNote(t, dir, "exists.md", "# Exists\n", mustParseTime(t, "2026-01-01T00:00:00Z"))

	store := New(dir)
	if err := store.Supersede("exists", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v for missing new, want ErrNotFound", err)
	}
	if err := store.Supersede("ghost", "exists"); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v for missing old, want ErrNotFound", err)
	}
}

func TestKnowledgePathShimDelegatesToFSKnowledge(t *testing.T) {
	// The package-level shim should produce the same path FSKnowledge does
	// for the same project, so existing callers (internal/prompt/prompt.go,
	// internal/tools/memory.go) keep behaving identically.
	cwd := t.TempDir()
	shimPath := KnowledgePath(cwd)
	directPath := NewKnowledge(filepath.Join(cwd, ".klaudia")).Path()
	if shimPath != directPath {
		t.Errorf("shim path %q != direct path %q", shimPath, directPath)
	}
}
