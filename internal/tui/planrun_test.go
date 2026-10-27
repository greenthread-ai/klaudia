package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/goal"
	"github.com/greenthread-ai/klaudia/internal/plan"
)

// awaitPlan puts a plan up for approval and returns the reply channel the
// ExitPlanMode tool would be blocked on.
func awaitPlan(t *testing.T) (*Model, chan bool) {
	t.Helper()
	m := newTestModel()
	m.resize(100, 30)
	reply := make(chan bool, 1)
	m.update(planMsg{plan: "## Plan\n\n1. one\n2. two\n", reply: reply})
	if m.state != stateAwaitingPlan {
		t.Fatalf("state = %v, want awaiting plan", m.state)
	}
	return m, reply
}

func TestPlanApprovalIsAPicker(t *testing.T) {
	m, _ := awaitPlan(t)
	view := stripANSI(m.bottomView())
	for _, want := range append([]string{"Approve this plan?"}, planChoices...) {
		if !strings.Contains(view, want) {
			t.Errorf("approval view lacks %q:\n%s", want, view)
		}
	}
}

// "y" still approves outright, as it always has.
func TestPlanYApprovesWithoutARun(t *testing.T) {
	m, reply := awaitPlan(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if !<-reply || m.loopPlan || m.loopRemaining != 0 {
		t.Errorf("y: approved and no run expected (loopPlan=%v remaining=%d)", m.loopPlan, m.loopRemaining)
	}
}

// The second row approves and starts a plan run whose first turn is the one
// already running.
func TestPlanKeepGoingStartsAPlanRun(t *testing.T) {
	m, reply := awaitPlan(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyDown})
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !<-reply {
		t.Fatal("not approved")
	}
	if !m.loopPlan || m.loopRemaining != goal.DefaultIterations || !m.loopSpanOpen {
		t.Errorf("plan run not started: loopPlan=%v remaining=%d spanOpen=%v", m.loopPlan, m.loopRemaining, m.loopSpanOpen)
	}
	if m.sess.PermissionMode != "autonomous" {
		t.Errorf("mode = %q, want autonomous", m.sess.PermissionMode)
	}
}

// "Keep planning" sends the user's reasons; the old "n" could only refuse.
func TestPlanKeepPlanningSendsFeedback(t *testing.T) {
	m, reply := awaitPlan(t)
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.state != stateAnsweringOther || !strings.Contains(stripANSI(m.bottomView()), "What should change?") {
		t.Fatalf("n should open the feedback box (state %v)", m.state)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateAwaitingPlan {
		t.Fatalf("Esc should return to the approval picker, state %v", m.state)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	typeRunes(m, "split step two")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if <-reply {
		t.Fatal("feedback approved the plan")
	}
	if got := peekSteerPrompt(m); got != "split step two" {
		t.Errorf("feedback queued for the model = %q", got)
	}
}

// A plan run continues while steps remain, verifies once they are all done
// even without the completion token, and then stops.
func TestPlanRunLoop(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	path := filepath.Join(t.TempDir(), "plan.md")
	m.plan = plan.Snapshot{Approved: true, Path: path, Items: []plan.Item{
		{Content: "one", Status: plan.StatusCompleted}, {Content: "two", Status: plan.StatusPending},
	}}
	m.startPlanRun(5, true)

	next := m.loopNext(agent.Result{Text: "did step one", NumTurns: 1, OutputTokens: 10}, nil)
	if next != goal.PlanIterationPrompt(path) {
		t.Fatalf("with a step left, next = %q", next)
	}
	m.plan.Items[1].Status = plan.StatusCompleted
	next = m.loopNext(agent.Result{Text: "did step two", NumTurns: 1, OutputTokens: 10}, nil)
	if next != goal.PlanVerificationPrompt(path) {
		t.Fatalf("all steps done should verify, next = %q", next)
	}
	if next = m.loopNext(agent.Result{Text: "verified", NumTurns: 1, OutputTokens: 10}, nil); next != "" {
		t.Fatalf("verified run should stop, next = %q", next)
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "plan complete (verified)") {
		t.Errorf("no completion line:\n%s", out)
	}
}

// During a goal run the panel shows the spec's checklist, not a stale plan.
func TestGoalRunPanelShowsTheSpec(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	m.plan = plan.Snapshot{Items: []plan.Item{{Content: "stale plan step", Status: plan.StatusInProgress}}}
	spec := filepath.Join(t.TempDir(), "GOAL.md")
	if err := os.WriteFile(spec, []byte("- [x] first box\n- [ ] second box\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.loopTotal, m.loopRemaining, m.loopSpecPath = 6, 5, spec
	m.refreshGoalList()
	view := stripANSI(m.bottomView())
	if !strings.Contains(view, "Goal 1/2 · iteration 2/6") || !strings.Contains(view, "second box") {
		t.Errorf("panel should show the goal checklist:\n%s", view)
	}
	if strings.Contains(view, "stale plan step") {
		t.Errorf("stale plan shown during a goal run:\n%s", view)
	}
}
