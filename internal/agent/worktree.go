package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree isolation for background writers.
//
// Two background sub-agents both editing the same working tree corrupt each
// other: one's half-written file is the other's input, and a `git checkout`
// from one throws away the other's uncommitted work. A read-only agent has no
// such problem and shares the tree. So a writer (see subagent.Type.MayWrite)
// gets its own git worktree — a second checkout of the same repository, on a
// detached HEAD, that git manages alongside the main one — and it is removed
// when the agent finishes.
//
// The provider is an interface so tests can substitute a fake for git; the
// default drives the git CLI, which is already how the rest of the codebase
// talks to git (see tui.gitOutput).

// WorktreeProvider creates an isolated working tree for a background writer and
// returns its directory and a cleanup func. cleanup is always safe to call
// (nil-safe at the call site is the caller's job); it removes the worktree.
type WorktreeProvider interface {
	Create(ctx context.Context, repoDir, id string) (dir string, cleanup func() error, err error)
}

// gitWorktrees creates worktrees with the git CLI. run is injectable so the
// unit tests do not need a real repository; the integration test uses one.
type gitWorktrees struct {
	// run executes `git <args>` in dir and returns combined output. Defaults to
	// the real git binary.
	run func(ctx context.Context, dir string, args ...string) (string, error)
	// base is where worktree directories are created. Defaults to the OS temp
	// dir; a worktree does not have to live inside the repo, and keeping it out
	// avoids it being seen as an untracked path in the parent tree.
	base string
}

// newGitWorktrees builds the default git-backed provider.
func newGitWorktrees() *gitWorktrees {
	return &gitWorktrees{run: runGit}
}

// NewGitWorktrees returns the default git-backed WorktreeProvider, for wiring a
// Spawner (WithWorktrees).
func NewGitWorktrees() WorktreeProvider { return newGitWorktrees() }

// runGit is the default git runner.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		c.Dir = dir
	}
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Create adds a detached-HEAD worktree of repoDir. It fails if repoDir is not a
// git repository, so the caller can decide whether to fall back to the shared
// tree — worktree isolation only makes sense where there is a repo to branch a
// checkout from.
func (g *gitWorktrees) Create(ctx context.Context, repoDir, id string) (string, func() error, error) {
	if repoDir == "" {
		return "", nil, fmt.Errorf("worktree: no repository directory")
	}
	if _, err := g.run(ctx, repoDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", nil, fmt.Errorf("worktree: %s is not a git work tree: %w", repoDir, err)
	}

	base := g.base
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "klaudia-subagent-"+sanitizeWorktreeID(id))
	// A stale directory from a crashed run would make `git worktree add` refuse;
	// clear it first. This is best-effort — add still reports a real conflict.
	_ = os.RemoveAll(dir)

	// --detach: the writer gets an independent HEAD, so it never contends over a
	// branch ref with the main checkout or another worktree.
	if _, err := g.run(ctx, repoDir, "worktree", "add", "--detach", dir, "HEAD"); err != nil {
		return "", nil, err
	}

	cleanup := func() error {
		// --force: the writer will have left changes; removal is meant to discard
		// the isolated tree, so uncommitted work in it is not a reason to keep it.
		_, err := g.run(context.Background(), repoDir, "worktree", "remove", "--force", dir)
		// Even if git's own bookkeeping removal fails, take the directory with us.
		_ = os.RemoveAll(dir)
		return err
	}
	return dir, cleanup, nil
}

// sanitizeWorktreeID keeps a worktree directory name to safe characters.
func sanitizeWorktreeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "anon"
	}
	return b.String()
}
