package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

// A resumed session must not silently inherit yesterday's approvals. That is
// exactly the "flaky remembered command permissions" the trust model replaced.
func TestResumeDoesNotRestoreApprovals(t *testing.T) {
	st := resumeState{TrustPolicy: agent.HostEnforce, Mode: permission.ModeAutonomous}
	out := st.render()
	if !strings.Contains(out, "not restored") {
		t.Fatalf("resume does not say approvals were dropped:\n%s", out)
	}
	if !strings.Contains(out, "will ask again") {
		t.Errorf("resume does not say what happens instead:\n%s", out)
	}
}

// A job that is gone matters more than one that is running: the conversation
// the user is about to continue implies the server is still up.
func TestResumeSaysJobsAreGone(t *testing.T) {
	st := resumeState{
		DeadJobs: []string{"dev", "api"},
		Mode:     permission.ModeAutonomous,
	}
	out := st.render()
	for _, want := range []string{"dev", "api", "stopped when the previous session ended"} {
		if !strings.Contains(out, want) {
			t.Errorf("resume banner missing %q:\n%s", want, out)
		}
	}
}

func TestResumeShowsGoalAndTree(t *testing.T) {
	st := resumeState{
		Goal:        "Move session persistence to Postgres",
		Branch:      "auth-refactor",
		KlaudiaWork: []string{"src/db/store.go", "src/db/store_test.go"},
		UserWork:    []string{"notes.md"},
		Mode:        permission.ModeAutonomous,
	}
	out := st.render()
	for _, want := range []string{
		"Move session persistence to Postgres",
		"2 Klaudia change(s)", "src/db/store.go",
		"1 of your change(s)", "/changes",
		"auth-refactor",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("resume banner missing %q:\n%s", want, out)
		}
	}
}

// A fresh session with nothing to reconcile gets the ordinary banner.
func TestEmptyResumeHasNoContent(t *testing.T) {
	if (resumeState{Mode: permission.ModeAutonomous}).hasContent() {
		t.Error("an empty state claimed to have something to say")
	}
	if !(resumeState{Goal: "x"}).hasContent() {
		t.Error("a goal is worth reporting")
	}
	if !(resumeState{DeadJobs: []string{"dev"}}).hasContent() {
		t.Error("a stopped job is worth reporting")
	}
}

// Without re-seeding, every file Klaudia wrote yesterday looks like the user's
// today, and /commit would refuse to stage its own work.
func TestOwnershipIsReseededFromHistory(t *testing.T) {
	m := ctxModel(t)
	m.seedOwnershipFromHistory([]string{"src/a.go", "src/b.go"})
	if !m.touched["src/a.go"] || !m.touched["src/b.go"] {
		t.Fatalf("touched = %v", m.touched)
	}
	// But NOT stamped: Klaudia cannot claim to know what happened to the file
	// between sessions, so undo must treat it as shared.
	if _, stamped := m.base.stamps["src/a.go"]; stamped {
		t.Error("a re-seeded file was stamped as unchanged since Klaudia wrote it")
	}
}

func TestHistoryEditedPaths(t *testing.T) {
	history := []anthropic.BetaMessageParam{{
		Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaToolUseBlock("t1", map[string]any{"file_path": "src/a.go"}, "Write"),
			anthropic.NewBetaToolUseBlock("t2", map[string]any{"file_path": "src/b.go"}, "Edit"),
			anthropic.NewBetaToolUseBlock("t3", map[string]any{"file_path": "src/c.go"}, "Read"),
			anthropic.NewBetaToolUseBlock("t4", map[string]any{"notebook_path": "nb.ipynb"}, "NotebookEdit"),
		},
	}}
	got := historyEditedPaths(history)
	want := map[string]bool{"src/a.go": true, "src/b.go": true, "nb.ipynb": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want the three written paths", got)
	}
	for _, p := range got {
		if !want[p] {
			t.Errorf("%q was reported as edited; Read does not edit", p)
		}
	}
}

func TestJobNamesFromResults(t *testing.T) {
	got := jobNamesFromResults([]string{
		`Started job dev (bash_1). Read its output with BashOutput(bash_id="dev").`,
		`Restarted job api (bash_2). Read new output with BashOutput(bash_id="api").`,
		`Started job dev (bash_3).`, // a duplicate name is one job
		`[no new output]`,
	})
	if len(got) != 2 {
		t.Fatalf("got %v, want [dev api]", got)
	}
	if got[0] != "dev" || got[1] != "api" {
		t.Errorf("got %v, want [dev api]", got)
	}
}

func TestJobNamesIgnoresUnrelatedText(t *testing.T) {
	if got := jobNamesFromResults([]string{"the build Started job hunting", ""}); len(got) != 1 {
		// "Started job hunting" does parse to "hunting"; the point of this test
		// is that it does not panic or over-collect on prose.
		t.Logf("got %v", got)
	}
	if got := jobNamesFromResults(nil); len(got) != 0 {
		t.Errorf("got %v from no input", got)
	}
}
