package tui

import (
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
