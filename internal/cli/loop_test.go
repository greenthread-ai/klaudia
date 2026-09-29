package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/goal"
)

// gitInit makes the project a repository with one commit on main.
func gitInit(t *testing.T, e *cliEnv) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"commit", "-q", "-m", "spec"},
	} {
		if out, err := gitRun(e.Dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// A goal the model reports complete stops the loop after that iteration, on
// its own branch, with a hint naming the branch to merge back into.
func TestLoopStopsWhenGoalComplete(t *testing.T) {
	m := newFakeModel(t, say("all done\n"+goal.CompleteToken))
	e := newCLIEnv(t, m)
	e.write("PRD.md", "# Build the widget\n\n- [x] widget\n")
	gitInit(t, e)

	r := e.run(nil, "--loop", "--dangerously-skip-permissions")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if got := gitBranch(e.Dir); got != "klaudia/goal-build-the-widget" {
		t.Errorf("branch = %q, want klaudia/goal-build-the-widget", got)
	}
	for _, want := range []string{"iteration 1/10", "goal complete in 1 iteration", "git merge klaudia/goal-build-the-widget"} {
		if !strings.Contains(r.Stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.Stderr)
		}
	}
	if n := len(m.Requests()); n != 1 {
		t.Errorf("model called %d times, want 1", n)
	}

	// Run again from the goal branch: it is reused rather than recreated, and
	// with no base to name, the hint just says where the work is.
	m2 := newFakeModel(t, say(goal.CompleteToken))
	t.Setenv("KLAUDIA_CUSTOM_ENDPOINT", m2.URL())
	r = e.run(nil, "--loop", "--dangerously-skip-permissions")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if !strings.Contains(r.Stderr, "on branch klaudia/goal-build-the-widget") || !strings.Contains(r.Stderr, "review it (git log / git diff)") {
		t.Errorf("resumed loop stderr:\n%s", r.Stderr)
	}
}

// Iterations that make no commits stop the loop after loopStallLimit, and a
// wrap-up turn records progress in the spec.
func TestLoopStopsAfterStalledIterations(t *testing.T) {
	m := newFakeModel(t)
	e := newCLIEnv(t, m)
	e.write("PRD.md", "# Stalling goal\n")
	gitInit(t, e)

	r := e.run(nil, "--loop", "--dangerously-skip-permissions", "--max-iterations", "8")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	for _, want := range []string{"no new commits for 3 iterations", "summarising progress", "goal not yet complete"} {
		if !strings.Contains(r.Stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.Stderr)
		}
	}
	// Three stalled iterations and one wrap-up.
	reqs := m.Requests()
	if len(reqs) != loopStallLimit+1 {
		t.Fatalf("model called %d times, want %d", len(reqs), loopStallLimit+1)
	}
	if !strings.Contains(reqs[len(reqs)-1].Raw(), "Progress") {
		t.Error("the last turn was not the wrap-up prompt")
	}
}

// Outside a git repository the loop cannot branch or detect stalls; it warns,
// runs to the iteration cap, and wraps up.
func TestLoopOutsideGitRunsToIterationCap(t *testing.T) {
	m := newFakeModel(t)
	e := newCLIEnv(t, m)
	e.write(".klaudia/GOAL.md", "# No repo goal\n")
	r := e.run(nil, "--loop", "--permission-mode", "autonomous", "--max-iterations", "2")
	if r.Err != nil {
		t.Fatal(r.dump())
	}
	if !strings.Contains(r.Stderr, "warning: not branching") {
		t.Errorf("no branching warning:\n%s", r.Stderr)
	}
	if strings.Contains(r.Stderr, "Work is on branch") {
		t.Error("a merge hint was printed for a branch that was never created")
	}
	if n := len(m.Requests()); n != 3 {
		t.Errorf("model called %d times, want 2 iterations + wrap-up", n)
	}
}

// The loop refuses to start when it could not do anything useful.
func TestLoopRefusesWithoutSpecOrUnattendedMode(t *testing.T) {
	t.Run("no spec", func(t *testing.T) {
		m := newFakeModel(t)
		e := newCLIEnv(t, m)
		r := e.run(nil, "--loop", "--dangerously-skip-permissions")
		if r.Err == nil || !strings.Contains(r.Err.Error(), "no goal spec found") {
			t.Fatalf("err = %v", r.Err)
		}
		if len(m.Requests()) != 0 {
			t.Error("model called without a spec")
		}
	})
	t.Run("mode that asks", func(t *testing.T) {
		m := newFakeModel(t)
		e := newCLIEnv(t, m)
		e.write("PRD.md", "# g\n")
		r := e.run(nil, "--loop", "--permission-mode", "plan")
		if r.Err == nil || !strings.Contains(r.Err.Error(), "cannot answer prompts") {
			t.Fatalf("err = %v", r.Err)
		}
		if len(m.Requests()) != 0 {
			t.Error("model called in a mode the loop cannot run")
		}
	})
}

// An API failure mid-loop ends the loop with that error rather than spinning.
func TestLoopStopsOnAPIError(t *testing.T) {
	m := newFakeModel(t, fakeTurn{Status: 400})
	e := newCLIEnv(t, m)
	e.write("PRD.md", "# g\n")
	r := e.run(nil, "--loop", "--dangerously-skip-permissions", "--max-iterations", "3")
	if r.Err == nil || r.Code != ExitError {
		t.Fatalf("want an error exit\n%s", r.dump())
	}
	if n := len(m.Requests()); n != 1 {
		t.Errorf("model called %d times after a failure, want 1", n)
	}
}
