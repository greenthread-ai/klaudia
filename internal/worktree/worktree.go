// Package worktree gives an agent its own checkout to write in, and brings the
// result back.
//
// Two agents editing one working tree is not a race in the usual sense — no
// data is corrupted — it is worse: both succeed. One writes a file, the other
// reads a half-finished version of it and reasons about it, a third rewrites
// the first one's edit, and every step reports success. Running children
// sequentially is the alternative, and it is the thing concurrency was for.
//
// So a sub-agent that can write gets a `git worktree` of its own. That is the
// now-standard answer: a second checkout is cheap (git shares the object
// database), it is a real directory so every tool works unchanged, and git
// already knows how to compare two of them.
//
// # The worktree is the parent's state, not HEAD's
//
// `git worktree add <dir> HEAD` alone would hand the child the last commit,
// which is the one state nobody asked about: the user's uncommitted work would
// be missing. A child told to "test the function I just wrote" would look at a
// tree without it, find nothing, and say so convincingly. So the worktree is
// seeded with the parent's tracked delta (`git diff HEAD`) and its untracked,
// non-ignored files, and that state is committed — in the worktree, on its
// detached HEAD, so no branch of the user's moves. That commit is the baseline
// the child's changes are measured against.
//
// Ignored files are deliberately not copied. node_modules and target/ are
// build output, they are what makes a copy expensive, and a tree that builds is
// not the same promise as a tree that matches.
//
// # Coming back
//
// Adopt diffs the child's work against the baseline and applies that patch to
// the parent tree with `git apply`, which touches the working tree and not the
// index — the index is the user's, the same reason /undo writes loose objects
// instead of stashing.
//
// `git apply` is all-or-nothing, which is the behaviour worth having: a patch
// that no longer fits is one whose file changed underneath us, and the honest
// answer is to say so rather than overwrite. On failure it is retried file by
// file, so one collision does not discard four good files, and the ones that
// did not fit are reported with their checkout left in place.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/greenthread-ai/klaudia/internal/gitprobe"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/greenthread-ai/klaudia/internal/session"
)

// Tree is one isolated checkout.
type Tree struct {
	// Root is the repository the checkout was cut from.
	Root string
	// Dir is the checkout itself.
	Dir string

	// base is the commit holding the parent's working state at creation. The
	// child's changes are everything that differs from it.
	base string
	// aliases are the spellings of Dir that may appear in the child's output:
	// the path itself and its symlink-resolved form (on macOS a temp or home
	// path commonly resolves elsewhere, and a tool that canonicalises would
	// otherwise report a path the parent cannot find).
	aliases []string
}

// Report is what Adopt did.
type Report struct {
	// Adopted are paths, relative to the repository root, now in the parent
	// tree.
	Adopted []string
	// Conflicted are paths left behind because the parent's copy no longer
	// matches what the child started from.
	Conflicted []string
}

// Empty reports whether the child changed nothing.
func (r Report) Empty() bool { return len(r.Adopted) == 0 && len(r.Conflicted) == 0 }

// Summary is one line for a human or a model: what came back, and what did not.
func (r Report) Summary() string {
	if r.Empty() {
		return "no file changes"
	}
	parts := make([]string, 0, 2)
	if n := len(r.Adopted); n > 0 {
		parts = append(parts, plural(n, "file")+" applied to the working tree")
	}
	if n := len(r.Conflicted); n > 0 {
		parts = append(parts, plural(n, "file")+" NOT applied (changed meanwhile): "+
			strings.Join(r.Conflicted, ", "))
	}
	return strings.Join(parts, "; ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// Supported reports whether root can have a worktree cut from it.
//
// Three ways it cannot, all of them ordinary: git is not installed, root is not
// a repository, or the repository has no commit yet — `git worktree add` needs
// something to check out, and a fresh `git init` has no HEAD. Each is a reason
// to share the tree as before, never a reason to refuse the work.
func Supported(ctx context.Context, root string) bool {
	if root == "" {
		return false
	}
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	out, err := run(ctx, root, nil, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		return false
	}
	_, err = run(ctx, root, nil, "rev-parse", "--verify", "--quiet", "HEAD")
	return err == nil
}

// staleAfter is how long an abandoned checkout survives. Adoption removes the
// checkout, so one that is still here was left by a session that was
// interrupted or crashed — and the useful window for looking at it by hand is
// days, not forever. Pruned lazily, when the next New is already doing git
// work, so nothing is paid at startup.
const staleAfter = 7 * 24 * time.Hour

// New cuts a checkout from root holding the same working state, and returns it.
// label is a human hint used in the directory name (the sub-agent type).
func New(ctx context.Context, root, label string) (*Tree, error) {
	dir, err := reserve(root, label)
	if err != nil {
		return nil, err
	}
	pruneStale(ctx, root, filepath.Dir(dir), staleAfter)

	if _, err := run(ctx, root, nil, "worktree", "add", "--detach", "--quiet", dir, "HEAD"); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}
	t := &Tree{Root: root, Dir: dir, aliases: aliases(dir)}
	if err := t.seed(ctx); err != nil {
		// A checkout that does not hold the parent's state is worse than none:
		// the child would silently work against the last commit.
		_ = t.Remove(ctx)
		return nil, err
	}
	return t, nil
}

// seed copies the parent's uncommitted work in and commits it as the baseline.
func (t *Tree) seed(ctx context.Context) error {
	// Tracked changes, staged and unstaged together: `diff HEAD` is the whole
	// delta, and it reads the parent's index without writing to it.
	patch, err := run(ctx, t.Root, nil, "diff", "HEAD", "--binary", "--no-renames")
	if err != nil {
		return fmt.Errorf("git diff HEAD: %w", err)
	}
	if len(bytes.TrimSpace(patch)) > 0 {
		if _, err := run(ctx, t.Dir, patch, "apply", "--binary", "--whitespace=nowarn", "-"); err != nil {
			return fmt.Errorf("seeding uncommitted changes: %w", err)
		}
	}
	if err := t.copyUntracked(ctx); err != nil {
		return err
	}

	if _, err := run(ctx, t.Dir, nil, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	// --no-verify because the repository's pre-commit hook is the user's, aimed
	// at the user's commits; running their linter every time a sub-agent spawns
	// is both slow and a side effect nobody asked for. gpgsign off for the same
	// reason, and because a signing prompt would hang the spawn.
	// --allow-empty: a clean parent tree has nothing to commit, and the
	// baseline still has to exist.
	commit := []string{
		"-c", "user.name=Klaudia", "-c", "user.email=noreply@greenthread.ai",
		"-c", "commit.gpgsign=false",
		"commit", "--quiet", "--no-verify", "--allow-empty",
		"-m", "klaudia: parent working tree at spawn",
	}
	if _, err := run(ctx, t.Dir, nil, commit...); err != nil {
		return fmt.Errorf("recording the baseline: %w", err)
	}
	head, err := run(ctx, t.Dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("git rev-parse: %w", err)
	}
	t.base = strings.TrimSpace(string(head))
	return nil
}

// copyUntracked brings over files git is not tracking and not ignoring —
// typically source the user created a minute ago and has not added. They are
// invisible to `git diff`, so without this a child asked about a brand-new file
// would report that it does not exist.
func (t *Tree) copyUntracked(ctx context.Context) error {
	out, err := run(ctx, t.Root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return fmt.Errorf("git ls-files: %w", err)
	}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		src := filepath.Join(t.Root, rel)
		info, err := os.Lstat(src)
		if err != nil || !info.Mode().IsRegular() {
			// Gone between listing and copying, or a symlink/socket. Not worth
			// failing a spawn over.
			continue
		}
		if err := copyFile(src, filepath.Join(t.Dir, rel), info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// Changed lists what the child changed, relative to the repository root.
func (t *Tree) Changed(ctx context.Context) ([]string, error) {
	if _, err := run(ctx, t.Dir, nil, "add", "-A"); err != nil {
		return nil, fmt.Errorf("git add: %w", err)
	}
	out, err := run(ctx, t.Dir, nil, "diff", "--cached", "--name-only", "--no-renames", "-z", t.base)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	var names []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" {
			names = append(names, rel)
		}
	}
	return names, nil
}

// adoptLocks serialises adoption per repository.
//
// Two children finishing at once would otherwise both run `git apply` against
// one tree. For different files that is harmless, but for the same file each
// apply reads, computes and writes — so the loser's version could land on top
// of the winner's with neither reporting a conflict. That is the precise
// failure the whole package exists to remove, so the last step does not get to
// reintroduce it. Held across a handful of git invocations, never across the
// child's run.
var adoptLocks sync.Map // repository root -> *sync.Mutex

func lockRepo(root string) func() {
	v, _ := adoptLocks.LoadOrStore(root, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// Adopt applies the child's changes to the parent tree.
//
// Nothing is staged and nothing is committed: `git apply` without --index
// touches files only, so the user's staging area is exactly as they left it.
func (t *Tree) Adopt(ctx context.Context) (Report, error) {
	defer lockRepo(t.Root)()

	names, err := t.Changed(ctx)
	if err != nil {
		return Report{}, err
	}
	if len(names) == 0 {
		return Report{}, nil
	}
	patch, err := run(ctx, t.Dir, nil, "diff", "--binary", "--no-renames", "--cached", t.base)
	if err != nil {
		return Report{}, fmt.Errorf("git diff: %w", err)
	}
	if len(bytes.TrimSpace(patch)) == 0 {
		// Changed can report a path whose content is identical (a mode or
		// mtime-only touch); there is then nothing to apply.
		return Report{}, nil
	}
	if _, err := run(ctx, t.Root, patch, "apply", "--binary", "--whitespace=nowarn", "-"); err == nil {
		return Report{Adopted: names}, nil
	}
	// The whole patch did not fit. Retry per file: usually one file moved
	// underneath us and the rest are fine.
	var rep Report
	for _, rel := range names {
		_, err := run(ctx, t.Root, patch, "apply", "--binary", "--whitespace=nowarn", "--include="+rel, "-")
		if err != nil {
			rep.Conflicted = append(rep.Conflicted, rel)
			continue
		}
		rep.Adopted = append(rep.Adopted, rel)
	}
	return rep, nil
}

// Remove deletes the checkout. It is best-effort by design: a leaked directory
// is a tidiness problem, and failing a sub-agent that has already done its work
// to report one would be a worse trade.
func (t *Tree) Remove(ctx context.Context) error {
	if _, err := run(ctx, t.Root, nil, "worktree", "remove", "--force", t.Dir); err == nil {
		return nil
	}
	// `git worktree remove` can refuse (a locked or already-broken checkout).
	// Delete the directory and let git reconcile its administrative entry,
	// otherwise the repository keeps a reference to a tree nobody can use.
	err := os.RemoveAll(t.Dir)
	run(ctx, t.Root, nil, "worktree", "prune") //nolint:errcheck // best effort
	return err
}

// Rewrite maps checkout paths in text back to the parent repository.
//
// The child reports real findings at paths that exist only inside its
// checkout, and both the parent model and the user would follow them to
// nothing. The directory name is long and unique enough that substituting it
// cannot collide with prose.
func (t *Tree) Rewrite(s string) string {
	if t == nil {
		return s
	}
	// Longest first. The resolved spelling usually *contains* the unresolved
	// one (/private/var/... holds /var/...), so substituting the short alias
	// first leaves the prefix behind: "/private/var/x/main.go" became
	// "/private<root>/main.go".
	for _, a := range longestFirst(t.aliases) {
		s = strings.ReplaceAll(s, a, t.Root)
	}
	return s
}

func longestFirst(in []string) []string {
	out := append([]string(nil), in...)
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// reserve picks a directory for a new checkout and creates its parent.
//
// Checkouts live under ~/.klaudia/worktrees/<project>/, never inside the
// project: a checkout in the project would be walked by the parent's own Glob
// and Grep, so the model would find two of every file and read a copy of the
// code it is editing. ~/.klaudia rather than TMPDIR so an interrupted run
// leaves something a person can still find, and because the trust classifier
// treats a plain path in $HOME as project work — a checkout under /etc-shaped
// policy would ask the user to approve every write.
//
// The leaf itself must not exist: `git worktree add` insists on creating it.
func reserve(root, label string) (string, error) {
	base := filepath.Join(session.ConfigRoot(), "worktrees", session.EncodePath(root))
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("worktree directory: %w", err)
	}
	slug := sanitize(label)
	if slug == "" {
		slug = "agent"
	}
	// A timestamp plus a counter: two children of the same type spawned in the
	// same second must not collide, and a bare timestamp did.
	stamp := time.Now().UTC().Format("20060102-150405")
	for i := 0; i < 100; i++ {
		dir := filepath.Join(base, fmt.Sprintf("%s-%s", slug, stamp))
		if i > 0 {
			dir = filepath.Join(base, fmt.Sprintf("%s-%s-%d", slug, stamp, i))
		}
		if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
			return dir, nil
		}
	}
	return "", errors.New("no free worktree directory")
}

// pruneStale removes checkouts abandoned by earlier runs.
func pruneStale(ctx context.Context, root, base string, age time.Duration) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-age)
	removed := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.RemoveAll(filepath.Join(base, e.Name())) == nil {
			removed = true
		}
	}
	if removed {
		run(ctx, root, nil, "worktree", "prune") //nolint:errcheck // best effort
	}
}

// sanitize reduces a label to something safe in a path.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func aliases(dir string) []string {
	out := []string{dir}
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		out = append(out, real)
	}
	return out
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// run executes git in dir, returning stdout. stderr is folded into the error so
// a caller can report what git actually said; callers that print output must
// not have it polluted with warnings.
func run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	// Guarded: these are Klaudia's own commands, and `git worktree add` runs
	// the repository's post-checkout hook (see gitprobe).
	cmd := exec.CommandContext(ctx, "git", gitprobe.Args(args...)...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	// A pager or an editor would block forever with no terminal to draw on.
	cmd.Env = append(os.Environ(), "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	err := cmd.Run()
	if err != nil && stderr.Len() > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), err
}
