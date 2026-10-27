//go:build darwin || linux

package e2e

import (
	"os"
	"os/exec"
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
	for _, want := range []string{"✓ Read main.go", "✓ Edit main.go"} {
		if !strings.Contains(snap.All(), want) {
			t.Errorf("%q not shown relative\n%s", want, snap.All())
		}
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

// A sub-agent sent to the background: the turn ends at once, the agent shows
// under the prompt while it works, and its result reaches the model as a turn
// of its own when it finishes.
func TestBackgroundAgentReportsBack(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "bgagent")
	tm.WaitFor(t, "the parent turn to finish while the agent runs", 20*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "Started it; carrying on.") && strings.Contains(s.Screen(), "⏵ agent_1")
	})
	snap := tm.WaitText(t, "Got the background result.", 30*time.Second)
	if !strings.Contains(snap.All(), "✓ background agent_1 (Explore: survey) finished") {
		t.Errorf("no completion line\n%s", snap.All())
	}
	found := false
	for _, p := range env.Mock.Prompts() {
		found = found || (strings.Contains(p, `<background-agent id="agent_1"`) && strings.Contains(p, "three findings"))
	}
	if !found {
		t.Errorf("the result never reached the model; prompts: %q", env.Mock.Prompts())
	}
}

// A project's .mcp.json used to launch its servers the moment Klaudia started
// in a fresh clone. The first turn now puts them to the user on the
// repository-config card; the answer is remembered, and the turn goes ahead
// either way.
func TestProjectMCPServersAskFirst(t *testing.T) {
	for _, tc := range []struct {
		name       string
		key        string
		remembered bool
	}{
		{"approved and remembered", "y", true},
		{"declined for the session", "n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newEnv(t)
			marker := filepath.Join(env.Tmp, "launched")
			if err := os.WriteFile(filepath.Join(env.Project, ".mcp.json"), []byte(
				`{"mcpServers":{"repo":{"command":"sh","args":["-c","touch `+marker+`"]}}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			tm := env.Start(t)
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("the project server was launched before anyone was asked")
			}
			env.prompts++
			tm.Type("hello")
			tm.Press(keyEnter)
			tm.WaitText(t, "Start these MCP servers?", 15*time.Second)
			snap := tm.Snapshot().All()
			if !strings.Contains(snap, "repo: sh -c touch") {
				t.Errorf("the card does not show the command:\n%s", snap)
			}
			tm.Type(tc.key)
			tm.WaitText(t, "Hello from the mock model.", 20*time.Second)

			_, err := os.Stat(marker)
			if launched := err == nil; launched != tc.remembered {
				t.Errorf("launched = %v after %q", launched, tc.key)
			}
			_, err = os.Stat(filepath.Join(env.Config, "mcp-approvals.json"))
			if saved := err == nil; saved != tc.remembered {
				t.Errorf("approval saved = %v after %q", saved, tc.key)
			}

			// Asked once: the next turn goes straight to the model.
			env.Prompt(t, tm, "hello again")
			if n := strings.Count(tm.Snapshot().All(), "Start these MCP servers?"); n > 1 {
				t.Errorf("asked %d times in one session", n)
			}
		})
	}
}

// /mcp with real servers on both sides of the trust line: a global one (the
// user's own) is connected at start, and a project one is listed as waiting —
// before, a held server was absent, and a project whose servers were all held
// read "No MCP servers configured". Approving from /mcp starts it, and the
// first turn then has nothing left to ask.
func TestMCPListsGlobalAndHeldServers(t *testing.T) {
	env := newEnv(t)
	write := func(path, name string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"mcpServers":{"`+name+`":{"command":"`+mcpServerBin+`","args":["`+name+`"]}}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(env.Config, ".mcp.json"), "godot")
	write(filepath.Join(env.Project, ".mcp.json"), "figma")
	tm := env.Start(t)

	tm.Type("/mcp")
	tm.Press(keyEnter)
	snap := tm.WaitText(t, "Manage MCP servers", 15*time.Second)
	for _, want := range []string{"● connected  godot (1 tool)", "◌ figma — waiting for approval", mcpServerBin + " figma"} {
		if !strings.Contains(snap.All(), want) {
			t.Errorf("/mcp lacks %q:\n%s", want, snap.All())
		}
	}
	tm.Press(keyDown) // row 1 is "Disconnect godot"
	tm.Press(keyEnter)
	tm.WaitText(t, "Approved 1 project server", 15*time.Second)

	tm.Type("/mcp")
	tm.Press(keyEnter)
	tm.WaitFor(t, "figma connected", 15*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "● connected  figma (1 tool)")
	})
	tm.Press(keyEsc)
	tm.WaitFor(t, "the picker to close", 5*time.Second, func(s Snapshot) bool {
		return !strings.Contains(s.All(), "esc to cancel")
	})

	env.Prompt(t, tm, "hello")
	tm.WaitText(t, "Hello from the mock model.", 20*time.Second)
	if strings.Contains(tm.Snapshot().All(), "Start these MCP servers?") {
		t.Error("the first turn asked about servers already approved from /mcp")
	}
}

// /rewind through the real binary: the tools scenario edits main.go; going
// back to before that prompt puts main.go back and the prompt in the box.
func TestRewindUndoesATurn(t *testing.T) {
	env := newEnv(t)
	// Checkpoints are git blobs: without a repository there is nothing to
	// restore, for /undo as for /rewind.
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=e@x", "-c", "user.name=e", "commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = env.Project
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	tm := env.Start(t)
	env.Prompt(t, tm, "use the tools")
	tm.WaitText(t, "Changed the greeting", 30*time.Second)
	if b, _ := os.ReadFile(filepath.Join(env.Project, "main.go")); !strings.Contains(string(b), "hello, e2e") {
		t.Fatalf("precondition: the scenario did not edit main.go:\n%s", b)
	}
	tm.Type("/rewind")
	tm.Press(keyEnter)
	tm.WaitText(t, "Rewind to before which prompt?", 10*time.Second)
	tm.Press(keyEnter) // the newest: "use the tools"
	tm.WaitText(t, "restore  main.go", 10*time.Second)
	tm.Type("y")
	tm.WaitText(t, "Rewound to before that prompt", 10*time.Second)
	if b, _ := os.ReadFile(filepath.Join(env.Project, "main.go")); string(b) != mainGo {
		t.Errorf("main.go was not restored:\n%s", b)
	}
	tm.WaitFor(t, "the prompt back in the box", 5*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "› use the tools") && s.InputBox()
	})
}

// --restricted runs on project files alone: it still answers (the project
// config names the provider), and leaves nothing in the user's config root.
func TestRestrictedLeavesNoState(t *testing.T) {
	env := newEnv(t)
	sessions := filepath.Join(env.Config, "sessions")
	cmd := exec.Command(klaudiaBin, "--restricted", "-p", "hello")
	cmd.Dir, cmd.Env = env.Project, env.environ()
	env.prompts++
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Hello from the mock model.") {
		t.Fatalf("restricted run: %v\n%s", err, out)
	}
	if entries, _ := os.ReadDir(sessions); len(entries) != 0 {
		t.Errorf("a restricted run wrote %d session dir(s) under the config root", len(entries))
	}
}

// /rewind's "summarise up to here": the conversation before the chosen prompt
// becomes a summary and the prompt stays, so the next request carries the
// summary and the kept prompt but not what was summarised.
func TestRewindSummarisesUpToHere(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "hello there")
	tm.WaitFor(t, "the first reply", 20*time.Second, func(s Snapshot) bool { return len(s.MatchLines(doneLine)) == 1 })
	env.Prompt(t, tm, "hello again, second prompt")
	tm.WaitFor(t, "the second reply", 20*time.Second, func(s Snapshot) bool { return len(s.MatchLines(doneLine)) == 2 })

	tm.Type("/rewind")
	tm.Press(keyEnter)
	tm.WaitText(t, "Rewind to before which prompt?", 10*time.Second)
	tm.Press(keyEnter) // the newest: the second prompt
	tm.WaitText(t, "Summarise everything before it", 10*time.Second)
	tm.Press(keyDown)
	tm.Press(keyEnter)
	tm.WaitText(t, "Summarised the conversation before that prompt", 20*time.Second)

	env.Prompt(t, tm, "hello, third")
	tm.WaitFor(t, "the third reply", 20*time.Second, func(s Snapshot) bool { return len(s.MatchLines(doneLine)) == 3 })
	body := env.Mock.LastRequestBody()
	if !strings.Contains(body, "SUMMARY: the user said hello") || !strings.Contains(body, "hello again, second prompt") {
		t.Errorf("the next request lacks the summary or the kept prompt:\n%s", body)
	}
	if strings.Contains(body, "hello there") {
		t.Errorf("the summarised prompt was still sent:\n%s", body)
	}
}
