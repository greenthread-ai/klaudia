package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ownedModel(t *testing.T, dir string) *Model {
	t.Helper()
	m := &Model{sess: &Session{CWD: dir}, base: newBaseline(), touched: map[string]bool{}}
	return m
}

// The whole point: a file dirty before Klaudia started is the user's, however
// it looks in git status now.
func TestPreexistingChangesStayTheUsers(t *testing.T) {
	dir := t.TempDir()
	m := ownedModel(t, dir)
	m.base.capture(" M docs/notes.md\n")
	m.touched["src/api.go"] = true

	files := m.classify(" M docs/notes.md\n M src/api.go\n?? scratch.txt\n")
	got := map[string]fileOwner{}
	for _, f := range files {
		got[f.Path] = f.Owner
	}
	if got["docs/notes.md"] != ownerUser {
		t.Error("a file dirty before the session was attributed to Klaudia")
	}
	if got["src/api.go"] != ownerKlaudia {
		t.Error("a file Klaudia wrote was not attributed to it")
	}
	if got["scratch.txt"] != ownerUser {
		t.Error("a file nobody claimed should default to the user")
	}
}

// A file Klaudia wrote that was ALSO already dirty belongs to both. Calling it
// Klaudia's would licence an undo that discards the user's half.
func TestFileDirtyBeforeAndWrittenBySharesOwnership(t *testing.T) {
	dir := t.TempDir()
	m := ownedModel(t, dir)
	m.base.capture(" M src/api.go\n")
	m.touched["src/api.go"] = true

	files := m.classify(" M src/api.go\n")
	if files[0].Owner != ownerBoth {
		t.Errorf("owner = %v, want ownerBoth", files[0].Owner)
	}
	if names := klaudiaOwned(files); len(names) != 0 {
		t.Errorf("a shared file was offered as safe to undo: %v", names)
	}
}

// §16: "manual edits made while Klaudia is working are reconciled". Klaudia
// cannot merge them, but it must not pretend they are not there.
func TestEditUnderneathIsNoticed(t *testing.T) {
	dir := t.TempDir()
	rel := "src/api.go"
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("klaudia wrote this\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	b := newBaseline()
	b.stamp(dir, rel)
	if b.changedUnderneath(dir, rel) {
		t.Fatal("an untouched file was reported as changed")
	}

	// The user edits it in another window.
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte("and then the user did\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !b.changedUnderneath(dir, rel) {
		t.Error("an edit underneath Klaudia went unnoticed")
	}

	// So did deleting it.
	os.Remove(path)
	if !b.changedUnderneath(dir, rel) {
		t.Error("a file removed underneath Klaudia went unnoticed")
	}
}

func TestEditUnderneathMakesOwnershipShared(t *testing.T) {
	dir := t.TempDir()
	rel := "a.go"
	os.WriteFile(filepath.Join(dir, rel), []byte("one\n"), 0o644)

	m := ownedModel(t, dir)
	m.touched[rel] = true
	m.base.stamp(dir, rel)

	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, rel), []byte("two\n"), 0o644)

	files := m.classify(" M a.go\n")
	if files[0].Owner != ownerBoth {
		t.Errorf("owner = %v, want ownerBoth after an edit underneath", files[0].Owner)
	}
}

func TestRenderOwnershipGroups(t *testing.T) {
	out := renderOwnership([]ownedFile{
		{Path: "src/config.ts", Owner: ownerUser, Status: " M"},
		{Path: "src/auth/session.ts", Owner: ownerKlaudia, Status: " M"},
		{Path: "shared.go", Owner: ownerBoth, Status: " M"},
	})
	for _, want := range []string{
		"Your existing changes", "src/config.ts",
		"Klaudia", "src/auth/session.ts",
		"Both", "shared.go", "undo will not touch these",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The user's own changes come first: they are the ones at risk.
	if strings.Index(out, "Your existing") > strings.Index(out, "Klaudia\n") {
		t.Errorf("Klaudia's changes were listed before the user's:\n%s", out)
	}
}

// A clean start should say nothing.
func TestNoWarningWhenTreeIsClean(t *testing.T) {
	m := ownedModel(t, t.TempDir())
	m.base.capture("")
	m.warnIfDirtyAtStart()
	if out := m.transcript.String(); out != "" {
		t.Errorf("a clean tree produced a warning: %q", out)
	}
}

func TestWarningNamesPreexistingFiles(t *testing.T) {
	m := ownedModel(t, t.TempDir())
	m.base.capture(" M a.go\n M b.go\n")
	m.warnIfDirtyAtStart()
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "a.go") || !strings.Contains(out, "2 file") {
		t.Errorf("warning = %q", out)
	}
	if !strings.Contains(out, "/changes") {
		t.Errorf("the warning does not say how to see the split: %q", out)
	}
}

// /commit must not sweep a shared file into a commit describing Klaudia's work.
func TestCommitSkipsSharedFiles(t *testing.T) {
	dir := t.TempDir()
	m := ownedModel(t, dir)
	m.base.capture(" M shared.go\n")
	m.touched["shared.go"] = true
	m.touched["mine.go"] = true

	status := " M shared.go\n M mine.go\n"
	safe := map[string]bool{}
	for _, f := range m.classify(status) {
		if f.Owner == ownerKlaudia {
			safe[f.Path] = true
		}
	}
	plan := planCommit(status, safe)
	if len(plan.Add) != 1 || plan.Add[0] != "mine.go" {
		t.Errorf("Add = %v, want only mine.go", plan.Add)
	}
	// And the exclusion is visible, not silent.
	if !strings.Contains(plan.describe(), "shared.go") {
		t.Errorf("the skipped file was not reported:\n%s", plan.describe())
	}
}
