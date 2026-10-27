package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The bug this replaced: `git add -A` swept the user's unrelated dirty files
// into a commit whose message described something else.
func TestCommitStagesOnlyWhatKlaudiaChanged(t *testing.T) {
	status := strings.Join([]string{
		" M internal/tui/tui.go",    // Klaudia edited this
		" M docs/notes.md",          // the user was in the middle of this
		"?? scratch.txt",            // the user's scratch file
		"?? internal/tui/commit.go", // Klaudia created this
	}, "\n")
	touched := map[string]bool{"internal/tui/tui.go": true, "internal/tui/commit.go": true}

	plan := planCommit(status, touched)
	if plan.Staged {
		t.Error("nothing was staged by the user, but the plan says otherwise")
	}
	if want := []string{"internal/tui/commit.go", "internal/tui/tui.go"}; !reflect.DeepEqual(plan.Add, want) {
		t.Errorf("Add = %v, want %v", plan.Add, want)
	}
	if want := []string{"docs/notes.md", "scratch.txt"}; !reflect.DeepEqual(plan.Skipped, want) {
		t.Errorf("Skipped = %v, want %v", plan.Skipped, want)
	}
	// The user has to be able to see what is being left behind.
	out := plan.describe()
	for _, want := range []string{"docs/notes.md", "scratch.txt", "Not included"} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt does not mention %q:\n%s", want, out)
		}
	}
}

// An explicit `git add` is a statement about what belongs in the commit.
// Adding to it would be the same mistake in the other direction.
func TestUserStagedChangesWinOutright(t *testing.T) {
	status := strings.Join([]string{
		"M  deliberately/staged.go",
		" M internal/tui/tui.go",
	}, "\n")
	plan := planCommit(status, map[string]bool{"internal/tui/tui.go": true})
	if !plan.Staged {
		t.Fatal("a staged file was not recognised")
	}
	if len(plan.Add) != 0 {
		t.Errorf("Add = %v; nothing should be added on top of the user's index", plan.Add)
	}
	// What Klaudia changed but left unstaged is being left out — say so rather
	// than let the user find out from the commit.
	if !strings.Contains(plan.describe(), "internal/tui/tui.go") {
		t.Errorf("the excluded file is not reported:\n%s", plan.describe())
	}
	if !strings.Contains(plan.describe(), "what you have staged") {
		t.Errorf("prompt does not say it is committing the index:\n%s", plan.describe())
	}
}

// With nothing attributable, /commit must not guess.
func TestNothingKlaudiaChangedIsNotACommit(t *testing.T) {
	plan := planCommit(" M docs/notes.md\n?? scratch.txt\n", nil)
	if !plan.empty() {
		t.Fatalf("plan is not empty: %+v", plan)
	}
	if !strings.Contains(plan.describe(), "Nothing to commit that Klaudia changed") {
		t.Errorf("describe = %q", plan.describe())
	}
}

// A rename reports both names; the new one is what gets staged.
func TestRenameStagesTheNewName(t *testing.T) {
	plan := planCommit(" R old/name.go -> new/name.go\n", map[string]bool{"new/name.go": true})
	if want := []string{"new/name.go"}; !reflect.DeepEqual(plan.Add, want) {
		t.Errorf("Add = %v, want %v", plan.Add, want)
	}
}

// git quotes paths with unusual bytes. Without unquoting, a touched file with a
// space in its name never matches and is silently left out of the commit.
func TestQuotedPathsMatch(t *testing.T) {
	plan := planCommit(" M \"docs/my notes.md\"\n", map[string]bool{"docs/my notes.md": true})
	if want := []string{"docs/my notes.md"}; !reflect.DeepEqual(plan.Add, want) {
		t.Errorf("Add = %v, want %v", plan.Add, want)
	}
}

func TestRelToRepo(t *testing.T) {
	m := &Model{sess: &Session{CWD: "/work/proj"}}
	for in, want := range map[string]string{
		"/work/proj/internal/a.go": "internal/a.go",
		"internal/a.go":            "internal/a.go",
		"/work/proj/a.go":          "a.go",
	} {
		if got := m.relToRepo(in); got != want {
			t.Errorf("relToRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

// A bare /commit drafts the message from the diff and still asks y/n; the
// draft names what it was given and what models wrap it in is stripped.
func TestCommitDraftsAMessage(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := gitOutput(dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q")
	git("-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "tui: an earlier subject")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644)

	m := newTestModel()
	m.sess.CWD = dir
	m.base = newBaseline()
	m.touched = map[string]bool{"a.go": true}
	m.sess.Suggest = func(context.Context, string) (string, error) {
		return "```\na: add the package\n```", nil
	}
	plan, ok := m.prepareCommit()
	if !ok {
		t.Fatal("nothing to commit")
	}
	prompt := commitPrompt(dir, plan, "make a package")
	for _, want := range []string{"tui: an earlier subject", "new file: a.go", "make a package"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	msg, _ := m.sess.Suggest(context.Background(), prompt)
	if got := cleanCommitMessage(msg); got != "a: add the package" {
		t.Errorf("cleaned = %q", got)
	}
	m.update(commitDraftMsg{plan: plan, message: "a: add the package"})
	if m.state != stateAwaitingConfirm || m.confirmAction == nil {
		t.Fatal("the draft did not lead to a y/n")
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "a: add the package") {
		t.Errorf("the draft was not shown:\n%s", out)
	}
	if got := m.confirmAction(); got != "Committed." {
		t.Fatalf("commit: %s", got)
	}
	if subj, _ := gitOutput(dir, "log", "-1", "--format=%s"); strings.TrimSpace(subj) != "a: add the package" {
		t.Errorf("committed subject = %q", subj)
	}
}
