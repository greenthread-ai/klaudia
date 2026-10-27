package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// /diff prints a short diff, names untracked files git diff leaves out, and
// says so when there is nothing.
func TestDiffNamesUntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := gitOutput(dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)
	run("add", "a.go")
	run("-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "-m", "a")

	m := newTestModel()
	m.resize(100, 30)
	m.sess.CWD = dir
	if cmd := m.showDiff(nil); cmd != nil || !strings.Contains(stripANSI(m.transcript.String()), "No changes.") {
		t.Fatalf("clean tree: %q", stripANSI(m.transcript.String()))
	}
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package b\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.go"), []byte("package n\n"), 0o644)
	m.showDiff(nil)
	out := stripANSI(m.transcript.String())
	for _, want := range []string{"-package a", "+package b", "New, untracked (not in the diff): new.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}
