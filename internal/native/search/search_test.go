package search

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"a.go":              "package a\nfunc Foo() {}\n",
		"b.go":              "package b\nfunc Bar() {}\n",
		"sub/c.go":          "package sub\nfunc Baz() {}\n",
		"sub/notes.txt":     "foo bar\nbaz\n",
		"node_modules/x.go": "should be ignored\n",
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	return dir
}

func TestGlobMatchesAndSkipsIgnored(t *testing.T) {
	dir := setupTree(t)
	got, err := Glob(GlobOptions{Root: dir, Pattern: "**/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 .go files (node_modules skipped), got %d: %v", len(got), got)
	}
	for _, p := range got {
		if filepath.Ext(p) != ".go" {
			t.Errorf("non-go file in results: %s", p)
		}
		if filepath.Base(filepath.Dir(p)) == "node_modules" {
			t.Errorf("node_modules not skipped: %s", p)
		}
	}
}

func TestGlobSortsByModTime(t *testing.T) {
	dir := t.TempDir()
	older := filepath.Join(dir, "old.go")
	newer := filepath.Join(dir, "new.go")
	os.WriteFile(older, []byte("x"), 0o644)
	os.WriteFile(newer, []byte("y"), 0o644)
	// Make newer genuinely newer.
	os.Chtimes(older, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))

	got, _ := Glob(GlobOptions{Root: dir, Pattern: "*.go"})
	if len(got) != 2 || filepath.Base(got[0]) != "new.go" {
		t.Errorf("expected new.go first, got %v", got)
	}
}

func TestGrepFilesAndContent(t *testing.T) {
	dir := setupTree(t)
	m, err := Grep(GrepOptions{Pattern: "func B", Root: dir, Glob: "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	// Bar and Baz match "func B".
	if len(m) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(m), m)
	}
	for _, hit := range m {
		if hit.Line == 0 {
			t.Errorf("expected line numbers in non-multiline mode: %+v", hit)
		}
	}
}

func TestGrepGlobMatchesNestedPaths(t *testing.T) {
	dir := setupTree(t)
	matches, err := Grep(GrepOptions{Pattern: "func", Root: dir, Glob: "sub/*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || filepath.Base(matches[0].File) != "c.go" {
		t.Fatalf("expected only sub/c.go, got %+v", matches)
	}
}

func TestGrepIgnoreCase(t *testing.T) {
	dir := setupTree(t)
	m, _ := Grep(GrepOptions{Pattern: "FOO", Root: dir, IgnoreCase: true})
	if len(m) == 0 {
		t.Error("expected case-insensitive match for FOO")
	}
}
