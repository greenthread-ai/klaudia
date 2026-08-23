package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// §17: undo must be trustworthy.
//
// "Trustworthy" here means something specific and narrow: it must be impossible
// for undo to destroy work the user did. Everything else — how much it can
// undo, how far back, how clever it is about hunks — is secondary to that one
// property, because an undo that occasionally eats an afternoon is worse than
// no undo at all. Nobody uses the second one by mistake.
//
// # Why git objects, and not a stash or the index
//
// Before a turn edits anything, Klaudia writes the *current* contents of each
// file it is about to touch into git's object database with `git hash-object
// -w`. That is a plain blob write: it does not touch the index, does not touch
// HEAD, does not create a stash entry, and is invisible to `git status`. The
// user's staging area is exactly as they left it.
//
// A stash would have been simpler and is wrong: `git stash` moves the *whole*
// working tree, including the user's unrelated edits, and popping it later can
// conflict. The index is wrong for the same reason — it belongs to the user.
//
// Restoring is then `git cat-file blob <sha> > path`, one file at a time, and
// only for files Klaudia owns outright (see ownership.go). A file the user also
// touched is skipped and said so.
//
// The blobs are reachable by sha and inspectable with ordinary git:
// `git cat-file -p <sha>` shows exactly what will come back. Nothing here is a
// format only Klaudia can read.

// checkpointStack is the undo stack.
//
// Mutex-guarded because it is written from the agent goroutine (via
// agent.Options.BeforeEdit, which has to run synchronously before the write)
// and read from the Bubble Tea loop.
type checkpointStack struct {
	mu    sync.Mutex
	items []checkpoint
}

func (s *checkpointStack) push(cp checkpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Successive edits within one turn extend that turn's checkpoint rather
	// than stacking: "undo" means undoing the operation the user asked for, not
	// the last individual file write inside it.
	if n := len(s.items); n > 0 && s.items[n-1].Label == cp.Label {
		s.items[n-1].Files = append(s.items[n-1].Files, cp.Files...)
		return
	}
	s.items = append(s.items, cp)
	if len(s.items) > maxCheckpoints {
		s.items = s.items[len(s.items)-maxCheckpoints:]
	}
}

func (s *checkpointStack) top() (checkpoint, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return checkpoint{}, false
	}
	return s.items[len(s.items)-1], true
}

func (s *checkpointStack) pop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) > 0 {
		s.items = s.items[:len(s.items)-1]
	}
}

func (s *checkpointStack) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// alreadyHave reports whether this turn's checkpoint already covers a path, so
// a file edited three times is snapshotted once — at its state before the
// first edit, which is the one undo should restore.
func (s *checkpointStack) alreadyHave(label, path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.items)
	if n == 0 || s.items[n-1].Label != label {
		return false
	}
	for _, f := range s.items[n-1].Files {
		if f.Path == path {
			return true
		}
	}
	return false
}

// checkpoint is one undoable operation.
type checkpoint struct {
	Label string // what the turn was doing, for the confirm prompt
	At    time.Time
	Files []checkpointFile
}

// checkpointFile is one file's prior contents.
type checkpointFile struct {
	Path string // repo-relative
	Blob string // git object sha of the contents before the change
	// New is true when the file did not exist before, so undoing means deleting
	// it rather than restoring a blob.
	New bool
}

// maxCheckpoints bounds the stack. Deep history is what git is for; this is the
// "I didn't mean that" button, and offering fifty of them would imply a
// guarantee about ordering that nothing here provides.
const maxCheckpoints = 10

// snapshotBefore records the current contents of paths about to be modified.
//
// Called with the paths a turn is about to touch. Failing to snapshot a file is
// not fatal — it just means that file cannot be undone, which is stated at undo
// time rather than discovered.
func (m *Model) snapshotBefore(label string, paths []string) {
	if m.sess == nil || m.sess.CWD == "" || len(paths) == 0 {
		return
	}
	cp := checkpoint{Label: label, At: time.Now()}
	for _, p := range paths {
		rel := m.relToRepo(p)
		if m.checkpoints.alreadyHave(label, rel) {
			continue // first state within the turn is the one to restore
		}
		full := filepath.Join(m.sess.CWD, rel)
		if _, err := os.Stat(full); err != nil {
			cp.Files = append(cp.Files, checkpointFile{Path: rel, New: true})
			continue
		}
		sha, err := gitOutput(m.sess.CWD, "hash-object", "-w", "--", rel)
		if err != nil {
			continue // not a repo, or unreadable: this file is simply not undoable
		}
		cp.Files = append(cp.Files, checkpointFile{Path: rel, Blob: strings.TrimSpace(sha)})
	}
	if len(cp.Files) == 0 {
		return
	}
	m.checkpoints.push(cp)
}

// undoPlan is what an undo would do, worked out before anything happens.
type undoPlan struct {
	Label    string
	Restore  []checkpointFile // files that will be put back
	Delete   []string         // files that will be removed (Klaudia created them)
	Skipped  []string         // files the user also touched
	Unmoved  []string         // files that have not changed since the checkpoint
	Checkout string           // the git command that would do the same thing, for the sceptical
}

// Empty reports whether there is nothing to undo.
func (p undoPlan) Empty() bool { return len(p.Restore) == 0 && len(p.Delete) == 0 }

// planUndo works out what the most recent checkpoint would restore.
//
// The plan is computed and shown before anything is written. §17's last
// checkbox is "the exact rollback is inspectable", and a confirmation that
// only says "undo 2 files?" is not inspectable — it is a promise.
func (m *Model) planUndo() (undoPlan, bool) {
	cp, ok := m.checkpoints.top()
	if !ok {
		return undoPlan{}, false
	}
	plan := undoPlan{Label: cp.Label}

	status, _ := gitOutput(m.sess.CWD, "status", "--porcelain")
	owner := map[string]fileOwner{}
	for _, f := range m.classify(status) {
		owner[f.Path] = f.Owner
	}

	for _, f := range cp.Files {
		// A file the user also changed is never touched. This is the property
		// the whole design exists to guarantee.
		if o, ok := owner[f.Path]; ok && o != ownerKlaudia {
			plan.Skipped = append(plan.Skipped, f.Path)
			continue
		}
		if f.New {
			if _, err := os.Stat(filepath.Join(m.sess.CWD, f.Path)); err == nil {
				plan.Delete = append(plan.Delete, f.Path)
			}
			continue
		}
		// Nothing to do if the file already matches the blob.
		if cur, err := gitOutput(m.sess.CWD, "hash-object", "--", f.Path); err == nil &&
			strings.TrimSpace(cur) == f.Blob {
			plan.Unmoved = append(plan.Unmoved, f.Path)
			continue
		}
		plan.Restore = append(plan.Restore, f)
	}
	if len(plan.Restore) > 0 {
		var shas []string
		for _, f := range plan.Restore {
			shas = append(shas, fmt.Sprintf("git cat-file -p %s > %s", f.Blob[:min(8, len(f.Blob))], f.Path))
		}
		plan.Checkout = strings.Join(shas, "\n")
	}
	return plan, true
}

// describe renders the plan for confirmation.
func (p undoPlan) describe() string {
	var b strings.Builder
	if p.Label != "" {
		fmt.Fprintf(&b, "Undo: %q\n", p.Label)
	} else {
		b.WriteString("Undo the last change\n")
	}
	for _, f := range p.Restore {
		fmt.Fprintf(&b, "  restore  %s\n", f.Path)
	}
	for _, f := range p.Delete {
		fmt.Fprintf(&b, "  delete   %s  (Klaudia created it)\n", f)
	}
	if len(p.Skipped) > 0 {
		b.WriteString("\nLeaving alone — you changed these too:\n")
		for _, f := range p.Skipped {
			b.WriteString("  · " + f + "\n")
		}
	}
	if len(p.Unmoved) > 0 {
		fmt.Fprintf(&b, "\n%d file(s) already match the checkpoint.\n", len(p.Unmoved))
	}
	if p.Checkout != "" {
		b.WriteString("\nEquivalent by hand:\n  " +
			strings.ReplaceAll(p.Checkout, "\n", "\n  ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// applyUndo carries out a plan. Returns a human summary.
func (m *Model) applyUndo(p undoPlan) string {
	restored, deleted := 0, 0
	var failed []string
	for _, f := range p.Restore {
		content, err := gitOutput(m.sess.CWD, "cat-file", "blob", f.Blob)
		if err != nil {
			failed = append(failed, f.Path)
			continue
		}
		full := filepath.Join(m.sess.CWD, f.Path)
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			failed = append(failed, f.Path)
			continue
		}
		restored++
		m.base.stamp(m.sess.CWD, f.Path)
	}
	for _, f := range p.Delete {
		if err := os.Remove(filepath.Join(m.sess.CWD, f)); err != nil {
			failed = append(failed, f)
			continue
		}
		deleted++
		delete(m.touched, f)
	}
	// Pop only after acting, so a failure leaves the checkpoint in place.
	m.checkpoints.pop()

	var parts []string
	if restored > 0 {
		parts = append(parts, fmt.Sprintf("restored %d file(s)", restored))
	}
	if deleted > 0 {
		parts = append(parts, fmt.Sprintf("deleted %d file(s)", deleted))
	}
	if len(p.Skipped) > 0 {
		parts = append(parts, fmt.Sprintf("left %d alone (you changed them)", len(p.Skipped)))
	}
	if len(failed) > 0 {
		parts = append(parts, "could not restore "+strings.Join(failed, ", "))
	}
	if len(parts) == 0 {
		return "Nothing changed."
	}
	return strings.ToUpper(parts[0][:1]) + parts[0][1:] + suffixList(parts[1:])
}

func suffixList(rest []string) string {
	if len(rest) == 0 {
		return "."
	}
	return ", " + strings.Join(rest, ", ") + "."
}

// undoCommand handles /undo.
func (m *Model) undoCommand() {
	if m.busyGuard("/undo") {
		return
	}
	if m.sess == nil || m.sess.CWD == "" {
		m.appendLine(errStyle.Render("no working directory"))
		return
	}
	plan, ok := m.planUndo()
	if !ok {
		m.appendLine(bannerStyle.Render("Nothing to undo — Klaudia hasn't changed anything this session."))
		return
	}
	if plan.Empty() {
		msg := "Nothing to undo."
		if len(plan.Skipped) > 0 {
			msg = "Nothing safe to undo: you have changed every file Klaudia touched.\n  " +
				strings.Join(plan.Skipped, "\n  ")
		}
		m.appendLine(bannerStyle.Render(msg))
		m.checkpoints.pop()
		return
	}

	m.appendLine(askStyle.Render(plan.describe()))
	m.confirmAction = func() string { return m.applyUndo(plan) }
	m.setState(stateAwaitingConfirm)
}

// beforeEdit is the agent loop's pre-write hook. Runs on the agent goroutine.
func (m *Model) beforeEdit(_ string, paths []string) {
	m.snapshotBefore(m.turnLabel, paths)
}
