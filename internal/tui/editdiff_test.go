package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A one-line change inside a three-line old_string used to be drawn as three
// "-" lines and three "+" lines with no position. It is one of each now,
// numbered where it sits in the file, with a line of context either side and
// the indentation kept.
func TestEditHunkIsNumberedWithContext(t *testing.T) {
	dir := t.TempDir()
	file := "package main\n\nfunc count() {\n\tfor _, w := range words(text) {\n\t\tout[w]++\n\t}\n}\n"
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(file), 0o644)
	m := newTestModel()
	m.sess.CWD = dir

	got := stripANSI(m.editHunk(filepath.Join(dir, "main.go"),
		"func count() {\n\tfor _, w := range strings.Split(text, \" \") {\n\t\tout[w]++",
		"func count() {\n\tfor _, w := range words(text) {\n\t\tout[w]++"))
	want := strings.Join([]string{
		"  3   func count() {",
		"  4 -     for _, w := range strings.Split(text, \" \") {",
		"  4 +     for _, w := range words(text) {",
		"  5           out[w]++",
	}, "\n")
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Context from the file itself when the strings share no lines, and a hunk
// without numbers when the file cannot be read.
func TestEditHunkFallsBack(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\ntwo\nNEW\nfour\n"), 0o644)
	m := newTestModel()
	m.sess.CWD = dir
	got := stripANSI(m.editHunk("a.go", "three", "NEW"))
	for _, want := range []string{"2   two", "3 - three", "3 + NEW", "4   four"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	got = stripANSI(m.editHunk("missing.go", "a", "b"))
	if got != "  - a\n  + b" {
		t.Errorf("unreadable file: %q", got)
	}
}
