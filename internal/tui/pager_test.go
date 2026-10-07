package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiffArgsColourFlagPrecedesUserArgs(t *testing.T) {
	if got, want := diffArgs(true, []string{"--stat"}), []string{"diff", "--color=always", "--stat"}; !reflect.DeepEqual(got, want) {
		t.Errorf("colour on: got %q, want %q", got, want)
	}
	// Off must be explicit, so a color.ui = always in git config cannot
	// override NO_COLOR.
	if got, want := diffArgs(false, nil), []string{"diff", "--no-color"}; !reflect.DeepEqual(got, want) {
		t.Errorf("colour off: got %q, want %q", got, want)
	}
}

func TestDiffColourHonoursNoColor(t *testing.T) {
	env := func(k string) string {
		if k == "NO_COLOR" {
			return "1"
		}
		return ""
	}
	if diffColour(env) {
		t.Error("NO_COLOR is set; /diff must not ask git for colour")
	}
}

// diffRepo makes a repository with one committed file and returns its path.
// The global and system git config are cut off so a user's settings cannot
// change what the test sees.
func diffRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestDiffShortPrintsInline(t *testing.T) {
	dir := diffRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel()
	m.sess.CWD = dir
	m.resize(80, 24)
	if cmd := m.showDiff(nil); cmd != nil {
		t.Error("a short diff should print inline, not launch a pager")
	}
	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "-one") || !strings.Contains(out, "+two") {
		t.Errorf("the diff should reach the transcript:\n%s", out)
	}
}

func TestDiffNoChanges(t *testing.T) {
	dir := diffRepo(t)
	m := newTestModel()
	m.sess.CWD = dir
	m.resize(80, 24)
	if cmd := m.showDiff(nil); cmd != nil {
		t.Error("an empty diff should not launch a pager")
	}
	if out := visibleText(m.transcript.String()); !strings.Contains(out, "No changes.") {
		t.Errorf("want \"No changes.\", got:\n%s", out)
	}
}

func TestDiffLongGoesToPager(t *testing.T) {
	dir := diffRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Repeat("line\n", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PAGER", "cat")
	t.Setenv("TMPDIR", t.TempDir()) // the pager's temp file is never cleaned up here
	m := newTestModel()
	m.sess.CWD = dir
	m.resize(80, 24)
	if cmd := m.showDiff(nil); cmd == nil {
		t.Fatal("a diff longer than two screens should go to the pager")
	}
	if strings.Contains(m.transcript.String(), "+line") {
		t.Error("a paged diff should not also be dumped into scrollback")
	}
}

func TestDiffErrorShowsGitMessage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	m := newTestModel()
	m.sess.CWD = t.TempDir()
	m.resize(80, 24)
	m.showDiff([]string{"--no-such-flag"})
	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "git diff:") || strings.Contains(out, "exit status") {
		t.Errorf("an error should carry git's own message, not the exit status:\n%s", out)
	}
}
