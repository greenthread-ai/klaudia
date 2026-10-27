//go:build darwin || linux

package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A question is a picker: typed-ahead text and its Enter do not answer it,
// the arrow keys move, and Enter takes the highlighted row.
func TestQuestionIsAPickerNotATextBox(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "question")
	tm.WaitText(t, "↑↓ or 1-3 to pick", 15*time.Second)

	tm.Type("also fix the tests")
	tm.Press(keyEnter)
	tm.WaitText(t, "typing doesn't answer", 5*time.Second)
	if env.Mock.Requests() != 1 {
		t.Fatalf("typed-ahead text answered the question (mock saw %d requests)\n%s", env.Mock.Requests(), tm.Snapshot())
	}

	tm.Press(keyDown)
	tm.WaitFor(t, "the highlight on row 2", 5*time.Second, func(s Snapshot) bool {
		return s.HasLine(regexp.MustCompile(`❯ 2\) hi`))
	})
	tm.Press(keyEnter)
	snap := tm.WaitFor(t, "the answer and the reply", 15*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "→ hi") && strings.Contains(s.All(), "Noted your choice.")
	})
	// Scrollback keeps the question and the answer, not the option list.
	if strings.Contains(snap.All(), "1) hello, world") {
		t.Errorf("the option list was left in scrollback\n%s", snap.All())
	}
}

// Over-wide lines wrap between words and keep their indent.
func TestLongLinesWrapAtWords(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "wrapme")
	snap := tm.WaitFor(t, "the failed command and the reply", 20*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "Done wrapping.") && s.HasLine(doneLine)
	})
	all := snap.All()
	for _, w := range strings.Fields(wrapSentence) {
		if !strings.Contains(all, w) {
			t.Errorf("%q was split across rows\n%s", w, all)
		}
	}
	for _, ln := range strings.Split(all, "\n") {
		if strings.Contains(ln, "papaword") && !strings.HasPrefix(ln, "  ") {
			t.Errorf("continuation row lost its indent: %q", ln)
		}
	}
}

// Paths under the project are shown relative to it.
func TestPathsAreShownRelative(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "use the tools")
	snap := tm.WaitFor(t, "the final reply", 30*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "Changed the greeting") && s.HasLine(doneLine)
	})
	if strings.Contains(snap.All(), env.Project+"/") {
		t.Errorf("an absolute project path reached the screen\n%s", snap.All())
	}
	if !strings.Contains(snap.All(), "Edited main.go") {
		t.Errorf("Edit result not shown relative\n%s", snap.All())
	}
}

// Plan mode end to end: the approval is answered, the model-facing result is
// folded to one line, the plan runs, and the saved plan is ticked off.
func TestPlanApprovalRuns(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	tm.Type("/plan")
	tm.Press(keyEnter)
	tm.WaitText(t, "Entered plan mode", 5*time.Second)
	env.Prompt(t, tm, "planit")
	tm.WaitText(t, "Approve this plan?", 15*time.Second)
	tm.Press("y")
	snap := tm.WaitFor(t, "the plan to finish", 20*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "Plan done.") && s.HasLine(doneLine)
	})
	all := snap.All()
	if !strings.Contains(all, "plan approved · saved to") {
		t.Errorf("approval not folded to one line\n%s", all)
	}
	if strings.Contains(all, "Record the plan's steps") {
		t.Errorf("the model-facing instructions reached the screen\n%s", all)
	}
	got, _ := os.ReadFile(filepath.Join(env.Project, "main.go"))
	if !strings.Contains(string(got), `"hello, plan"`) {
		t.Errorf("the approved plan did not run: main.go is\n%s", got)
	}
	plans, _ := filepath.Glob(filepath.Join(env.Config, "plans", "*.md"))
	if len(plans) != 1 {
		t.Fatalf("want one saved plan, got %v", plans)
	}
	if body, _ := os.ReadFile(plans[0]); strings.Contains(string(body), "- [ ]") {
		t.Errorf("saved plan still has open steps:\n%s", body)
	}
}

// /goal run iterates until the spec is ticked off, verifies, and prints one
// completion block for the whole run rather than one per iteration.
func TestGoalRunCompletesWithOneSummary(t *testing.T) {
	env := newEnv(t)
	spec := "# Greeting polish\n\n## Acceptance criteria\n- [ ] Greeting\n- [ ] README\n\n## Progress\n- [ ] Phase 1: greeting\n"
	if err := os.WriteFile(filepath.Join(env.Project, ".klaudia", "GOAL.md"), []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := env.Start(t)
	env.prompts++ // the loop's prompts reach the mock; count it for the isolation check
	tm.Type("/goal run 6")
	tm.Press(keyEnter)
	snap := tm.WaitText(t, "goal complete (verified)", 40*time.Second)
	body, _ := os.ReadFile(filepath.Join(env.Project, ".klaudia", "GOAL.md"))
	if strings.Contains(string(body), "- [ ]") {
		t.Errorf("spec still has open items:\n%s", body)
	}
	if n := strings.Count(snap.All(), "/diff to review"); n > 1 {
		t.Errorf("completion block printed %d times; want once for the run\n%s", n, snap.All())
	}
}

// Found while building this harness: a one-paragraph reply is one long line,
// and the live preview showed only its first row until the stream ended. The
// preview must follow the newest text while the turn is still running.
func TestStreamingPreviewFollowsALongParagraph(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "longpara")
	snap := tm.WaitFor(t, "a late word on screen mid-stream", 20*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.Screen(), "w150")
	})
	if snap.HasLine(doneLine) {
		t.Fatalf("the turn had already finished; the preview was not observed mid-stream\n%s", snap)
	}
}

// A plan approved with "keep going" outlives the model stopping halfway: the
// run sends it back to the open step, then verifies, then ends.
func TestPlanKeepGoingFinishesAnAbandonedPlan(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	tm.Type("/plan")
	tm.Press(keyEnter)
	tm.WaitText(t, "Entered plan mode", 5*time.Second)
	env.Prompt(t, tm, "halfplan")
	tm.WaitText(t, "Approve this plan?", 15*time.Second)
	tm.Press("2")
	snap := tm.WaitText(t, "plan complete (verified)", 30*time.Second)
	all := snap.All()
	for _, want := range []string{"Pausing after the first step.", "↻ turn 2/", "Finished the second step."} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q\n%s", want, all)
		}
	}
	plans, _ := filepath.Glob(filepath.Join(env.Config, "plans", "*.md"))
	if len(plans) != 1 {
		t.Fatalf("want one saved plan, got %v", plans)
	}
	if body, _ := os.ReadFile(plans[0]); strings.Contains(string(body), "- [ ]") {
		t.Errorf("plan still has open steps:\n%s", body)
	}
}

// "Keep planning" gets the user's reasons to the model.
func TestPlanKeepPlanningReachesTheModel(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	tm.Type("/plan")
	tm.Press(keyEnter)
	tm.WaitText(t, "Entered plan mode", 5*time.Second)
	env.Prompt(t, tm, "halfplan")
	tm.WaitText(t, "Approve this plan?", 15*time.Second)
	tm.Press("n")
	tm.WaitText(t, "What should change?", 5*time.Second)
	tm.Type("split the test step in two")
	tm.Press(keyEnter)
	tm.WaitFor(t, "the turn to end", 20*time.Second, func(s Snapshot) bool { return s.HasLine(doneLine) })
	found := false
	for _, p := range env.Mock.Prompts() {
		found = found || strings.Contains(p, "split the test step in two")
	}
	if !found {
		t.Errorf("the feedback never reached the model; prompts: %q", env.Mock.Prompts())
	}
}

// A background job is listed under the prompt while idle, and its elapsed time
// moves without any input — the panel ticks while something runs.
func TestRunningJobsPanel(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "bgjob")
	jobLine := regexp.MustCompile(`⏵ .* · (\d+)s`)
	first := tm.WaitFor(t, "the job under the prompt", 20*time.Second, func(s Snapshot) bool {
		return s.HasLine(doneLine) && s.HasLine(jobLine)
	})
	before := jobLine.FindStringSubmatch(strings.Join(first.MatchLines(jobLine), "\n"))[1]
	tm.WaitFor(t, "the elapsed time to move while idle", 5*time.Second, func(s Snapshot) bool {
		m := jobLine.FindStringSubmatch(strings.Join(s.MatchLines(jobLine), "\n"))
		return m != nil && m[1] != before
	})
	if !strings.Contains(first.StatusLine(), "job") {
		t.Errorf("status line should still count the job: %q", first.StatusLine())
	}
}
