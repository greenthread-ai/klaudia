package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// undoRepo builds a real git repo. Undo writes real git objects, so faking git
// would only test the fake.
func undoRepo(t *testing.T) (*Model, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "T"},
	} {
		if out, err := gitOutput(dir, args...); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	write(t, dir, "app.go", "original\n")
	write(t, dir, "other.go", "untouched\n")
	if out, err := gitOutput(dir, "add", "-A"); err != nil {
		t.Fatalf("git add: %s %v", out, err)
	}
	if out, err := gitOutput(dir, "commit", "-qm", "init"); err != nil {
		t.Fatalf("git commit: %s %v", out, err)
	}

	m := &Model{sess: &Session{CWD: dir}, base: newBaseline(), touched: map[string]bool{}}
	status, _ := gitOutput(dir, "status", "--porcelain")
	m.base.capture(status)
	return m, dir
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

// The basic contract: Klaudia changes a file, undo puts it back.
func TestUndoRestoresAFile(t *testing.T) {
	m, dir := undoRepo(t)

	m.snapshotBefore("fix the thing", []string{"app.go"})
	write(t, dir, "app.go", "klaudia changed this\n")
	m.noteTouched("app.go")

	plan, ok := m.planUndo()
	if !ok || plan.Empty() {
		t.Fatalf("nothing to undo: %+v", plan)
	}
	if got := m.applyUndo(plan); !strings.Contains(strings.ToLower(got), "restored 1") {
		t.Errorf("applyUndo said %q", got)
	}
	if got := read(t, dir, "app.go"); got != "original\n" {
		t.Errorf("file = %q, want the original contents", got)
	}
}

// The property the whole design exists for: undo must not be able to destroy
// the user's work.
func TestUndoRefusesFilesTheUserAlsoChanged(t *testing.T) {
	m, dir := undoRepo(t)

	m.snapshotBefore("edit both", []string{"app.go"})
	write(t, dir, "app.go", "klaudia changed this\n")
	m.noteTouched("app.go")

	// The user then edits the same file by hand.
	write(t, dir, "app.go", "klaudia changed this\nand the user added a line\n")

	plan, ok := m.planUndo()
	if !ok {
		t.Fatal("no checkpoint")
	}
	if len(plan.Restore) != 0 {
		t.Errorf("undo would have overwritten a file the user edited: %+v", plan.Restore)
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0] != "app.go" {
		t.Errorf("Skipped = %v, want [app.go]", plan.Skipped)
	}
	m.applyUndo(plan)
	if got := read(t, dir, "app.go"); !strings.Contains(got, "the user added a line") {
		t.Fatalf("THE USER'S WORK WAS DESTROYED: %q", got)
	}
}

// A file that was already dirty before the session is the user's, even if
// Klaudia later wrote it.
func TestUndoRefusesFilesDirtyBeforeTheSession(t *testing.T) {
	m, dir := undoRepo(t)
	// Dirty it, then re-capture the baseline as a startup would.
	write(t, dir, "app.go", "user was mid-edit\n")
	m.base = newBaseline()
	status, _ := gitOutput(dir, "status", "--porcelain")
	m.base.capture(status)

	m.snapshotBefore("touch it too", []string{"app.go"})
	write(t, dir, "app.go", "user was mid-edit\nplus klaudia\n")
	m.noteTouched("app.go")

	plan, _ := m.planUndo()
	if len(plan.Restore) != 0 {
		t.Errorf("undo would have discarded a pre-existing edit: %+v", plan.Restore)
	}
	m.applyUndo(plan)
	if got := read(t, dir, "app.go"); !strings.Contains(got, "user was mid-edit") {
		t.Fatalf("the user's in-progress edit was lost: %q", got)
	}
}

// A file Klaudia created is removed rather than restored to nothing.
func TestUndoDeletesFilesKlaudiaCreated(t *testing.T) {
	m, dir := undoRepo(t)

	m.snapshotBefore("add a file", []string{"new.go"})
	write(t, dir, "new.go", "brand new\n")
	m.noteTouched("new.go")

	plan, _ := m.planUndo()
	if len(plan.Delete) != 1 || plan.Delete[0] != "new.go" {
		t.Fatalf("Delete = %v, want [new.go]", plan.Delete)
	}
	m.applyUndo(plan)
	if _, err := os.Stat(filepath.Join(dir, "new.go")); !os.IsNotExist(err) {
		t.Error("the created file survived undo")
	}
}

// Undo touches nothing outside the checkpoint.
func TestUndoLeavesUnrelatedFilesAlone(t *testing.T) {
	m, dir := undoRepo(t)
	m.snapshotBefore("edit app", []string{"app.go"})
	write(t, dir, "app.go", "changed\n")
	m.noteTouched("app.go")
	write(t, dir, "other.go", "the user edited this separately\n")

	plan, _ := m.planUndo()
	m.applyUndo(plan)
	if got := read(t, dir, "other.go"); got != "the user edited this separately\n" {
		t.Errorf("an unrelated file was modified: %q", got)
	}
}

// Several edits to one file in one turn restore to the state before the first.
func TestUndoRestoresToTheStartOfTheTurn(t *testing.T) {
	m, dir := undoRepo(t)

	// Each step mirrors the real order: snapshot, write, then claim ownership.
	m.snapshotBefore("multi-step", []string{"app.go"})
	write(t, dir, "app.go", "step one\n")
	m.noteTouched("app.go")
	m.snapshotBefore("multi-step", []string{"app.go"}) // same turn, second edit
	write(t, dir, "app.go", "step two\n")
	m.noteTouched("app.go")

	if n := m.checkpoints.len(); n != 1 {
		t.Errorf("%d checkpoints for one turn, want 1", n)
	}
	plan, _ := m.planUndo()
	m.applyUndo(plan)
	if got := read(t, dir, "app.go"); got != "original\n" {
		t.Errorf("file = %q, want the state before the turn began", got)
	}
}

// Two turns are two undos.
func TestUndoIsPerTurn(t *testing.T) {
	m, dir := undoRepo(t)

	m.snapshotBefore("turn one", []string{"app.go"})
	write(t, dir, "app.go", "after one\n")
	m.noteTouched("app.go")

	m.snapshotBefore("turn two", []string{"app.go"})
	write(t, dir, "app.go", "after two\n")
	m.noteTouched("app.go")

	if n := m.checkpoints.len(); n != 2 {
		t.Fatalf("%d checkpoints, want 2", n)
	}
	plan, _ := m.planUndo()
	m.applyUndo(plan)
	if got := read(t, dir, "app.go"); got != "after one\n" {
		t.Errorf("first undo gave %q, want the state after turn one", got)
	}
	plan, _ = m.planUndo()
	m.applyUndo(plan)
	if got := read(t, dir, "app.go"); got != "original\n" {
		t.Errorf("second undo gave %q, want the original", got)
	}
}

// §17: "the exact rollback is inspectable". A confirmation that says "undo 2
// files?" is a promise, not an inspection.
func TestUndoPlanIsInspectable(t *testing.T) {
	m, dir := undoRepo(t)
	m.snapshotBefore("fix the refresh-token race", []string{"app.go"})
	write(t, dir, "app.go", "changed\n")
	m.noteTouched("app.go")

	plan, _ := m.planUndo()
	out := plan.describe()
	for _, want := range []string{"fix the refresh-token race", "restore", "app.go", "git cat-file"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan description missing %q:\n%s", want, out)
		}
	}
}

// Undo must not touch the index. The user's staging area is theirs.
func TestUndoDoesNotDisturbStaging(t *testing.T) {
	m, dir := undoRepo(t)
	write(t, dir, "other.go", "deliberately staged\n")
	if out, err := gitOutput(dir, "add", "other.go"); err != nil {
		t.Fatalf("git add: %s %v", out, err)
	}
	before, _ := gitOutput(dir, "diff", "--cached", "--name-only")

	m.snapshotBefore("edit app", []string{"app.go"})
	write(t, dir, "app.go", "changed\n")
	m.noteTouched("app.go")
	plan, _ := m.planUndo()
	m.applyUndo(plan)

	after, _ := gitOutput(dir, "diff", "--cached", "--name-only")
	if before != after {
		t.Errorf("the staging area changed: %q → %q", before, after)
	}
	if got := read(t, dir, "other.go"); got != "deliberately staged\n" {
		t.Errorf("a staged file was modified: %q", got)
	}
}

// No stash entries either — a stash would move the whole working tree.
func TestUndoCreatesNoStash(t *testing.T) {
	m, dir := undoRepo(t)
	m.snapshotBefore("edit", []string{"app.go"})
	write(t, dir, "app.go", "changed\n")
	m.noteTouched("app.go")
	plan, _ := m.planUndo()
	m.applyUndo(plan)

	if out, _ := gitOutput(dir, "stash", "list"); strings.TrimSpace(out) != "" {
		t.Errorf("undo left stash entries: %q", out)
	}
}

func TestNothingToUndo(t *testing.T) {
	m, _ := undoRepo(t)
	if _, ok := m.planUndo(); ok {
		t.Error("planUndo reported a checkpoint on a fresh session")
	}
	m.undoCommand()
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "Nothing to undo") {
		t.Errorf("output = %q", out)
	}
}

// Outside a repo there is no object database, so nothing is undoable — and it
// must fail quietly rather than pretend.
func TestUndoOutsideAGitRepo(t *testing.T) {
	dir := t.TempDir()
	m := &Model{sess: &Session{CWD: dir}, base: newBaseline(), touched: map[string]bool{}}
	write(t, dir, "a.txt", "x\n")
	m.snapshotBefore("edit", []string{"a.txt"})
	if n := m.checkpoints.len(); n != 0 {
		t.Errorf("%d checkpoints outside a repo, want 0", n)
	}
}
