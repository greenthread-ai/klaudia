package gitprobe

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A repository whose config names programs for git to run: an fsmonitor hook
// (every status) and an external diff driver (every diff). A probe must not
// run either; plain git, for contrast, runs the fsmonitor.
func TestProbeDoesNotRunRepoPrograms(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho x >> "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "f.txt")
	git("commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("config", "core.fsmonitor", hook)
	git("config", "diff.external", hook)

	for _, args := range [][]string{{"status", "--porcelain"}, {"diff"}, {"rev-parse", "--abbrev-ref", "HEAD"}} {
		if out, err := Command(dir, args...).CombinedOutput(); err != nil {
			t.Fatalf("probe %v: %v\n%s", args, err, out)
		}
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a probe ran a program named in the repository's config")
	}

	// Contrast: plain git status runs the fsmonitor, so the test above can fail.
	c := exec.Command("git", "status", "--porcelain")
	c.Dir = dir
	_ = c.Run()
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git does not run core.fsmonitor as a command; contrast not observable")
	}
}

// A repository's hooks do not run through a probe: `git worktree add`, which
// Klaudia runs for a writing sub-agent, would otherwise run post-checkout.
func TestProbeDoesNotRunRepoHooks(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	run := func(c *exec.Cmd) {
		t.Helper()
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=e", "GIT_AUTHOR_EMAIL=e@x", "GIT_COMMITTER_NAME=e", "GIT_COMMITTER_EMAIL=e@x")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", c.Args, err, out)
		}
	}
	plain := func(args ...string) *exec.Cmd { c := exec.Command("git", args...); c.Dir = dir; return c }
	run(plain("init", "-q"))
	run(plain("commit", "-q", "--allow-empty", "-m", "init"))
	hook := filepath.Join(dir, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho x >> "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(Command(dir, "worktree", "add", "-q", filepath.Join(t.TempDir(), "wt")))
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the repository's post-checkout hook ran through a probe")
	}
	run(plain("worktree", "add", "-q", filepath.Join(t.TempDir(), "wt2")))
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git does not run post-checkout on worktree add; contrast not observable")
	}
}
