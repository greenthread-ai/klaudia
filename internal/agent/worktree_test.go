package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// tempGitRepo initialises a real git repository with one commit, so worktree
// operations run against genuine git rather than a stub. Skips when git is
// absent, keeping the suite runnable without it.
func tempGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		// Deterministic identity so `git commit` does not depend on global config.
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "seed")
	return dir
}

// The git-backed provider creates an isolated worktree, a write in it does not
// touch the parent tree, and cleanup removes it.
func TestGitWorktreeCreateIsolatesAndCleansUp(t *testing.T) {
	repo := tempGitRepo(t)
	g := newGitWorktrees()
	g.base = t.TempDir() // keep the worktree out of the OS temp root

	dir, cleanup, err := g.Create(context.Background(), repo, "agent-7")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dir == repo {
		t.Fatal("worktree shares the parent directory")
	}
	// The seed commit is present (it is a checkout of the same repo)...
	if _, err := os.Stat(filepath.Join(dir, "seed.txt")); err != nil {
		t.Errorf("worktree is not a checkout of the repo: %v", err)
	}
	// ...and a write inside the worktree does not appear in the parent tree.
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "scratch.txt")); !os.IsNotExist(err) {
		t.Errorf("a write in the worktree leaked into the parent tree (err=%v)", err)
	}

	if err := cleanup(); err != nil {
		t.Errorf("cleanup: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("worktree directory survived cleanup (err=%v)", err)
	}
}

// Isolation only makes sense in a git repository; a plain directory is refused
// so the caller can react rather than get a broken worktree.
func TestGitWorktreeRefusesNonRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	g := newGitWorktrees()
	if _, _, err := g.Create(context.Background(), t.TempDir(), "agent-1"); err == nil {
		t.Fatal("expected Create to refuse a non-git directory")
	}
}
