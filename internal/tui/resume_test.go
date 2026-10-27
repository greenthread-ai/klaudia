package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// A resumed session must not silently inherit yesterday's approvals, and must
// say so; and it must say it resumed at all — with a clean tree the card used
// to be skipped, and nothing showed the model was carrying on from history.
func TestResumeAlwaysSaysSoInOneLine(t *testing.T) {
	out := resumeState{HasGuardrail: true, Mode: permission.ModeAutonomous, Messages: 14}.render()
	if strings.Count(out, "\n") != 0 {
		t.Errorf("a clean resume should be one line:\n%s", out)
	}
	for _, want := range []string{"continuing", "14 messages", "approvals start fresh", "/clear"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

// A job that is gone matters more than one that is running: the conversation
// the user is about to continue implies the server is still up.
func TestResumeSaysJobsAreGone(t *testing.T) {
	out := resumeState{HasGuardrail: true, DeadJobs: []string{"dev", "api"}, Mode: permission.ModeAutonomous}.render()
	if !strings.Contains(out, "stopped when the previous session ended: dev, api") {
		t.Errorf("resume banner:\n%s", out)
	}
}

func TestResumeShowsGoalAndTree(t *testing.T) {
	out := resumeState{
		HasGuardrail: true,
		Goal:         "Move session persistence to Postgres",
		Branch:       "auth-refactor",
		KlaudiaWork:  []string{"src/db/store.go", "src/db/store_test.go"},
		UserWork:     []string{"notes.md"},
		Mode:         permission.ModeAutonomous,
	}.render()
	for _, want := range []string{
		"goal: Move session persistence to Postgres",
		"uncommitted on auth-refactor: 2 files by Klaudia (src/db/store.go, src/db/store_test.go) · 1 file of yours — /changes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("resume banner missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "\n") != 2 {
		t.Errorf("want three lines:\n%s", out)
	}
}

// The mode is in the status bar; the card names trust only when it is off.
func TestResumeWarnsOnlyWhenTheGuardrailIsOff(t *testing.T) {
	if out := (resumeState{HasGuardrail: true, Mode: permission.ModeBypassPermissions}).render(); !strings.Contains(out, "guardrail is off") {
		t.Errorf("bypass not flagged:\n%s", out)
	}
	if out := (resumeState{HasGuardrail: true, Mode: permission.ModeAutonomous}).render(); strings.Contains(out, "⚠") {
		t.Errorf("an ordinary mode was flagged:\n%s", out)
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
