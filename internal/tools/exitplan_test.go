package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/plan"
)

type fakePlanner struct {
	gotPlan  string
	approved bool
}

func (f *fakePlanner) ExitPlan(_ context.Context, plan string) (bool, error) {
	f.gotPlan = plan
	return f.approved, nil
}

func newExitPlan(t *testing.T) (*ExitPlanMode, *plan.Store) {
	t.Helper()
	store := plan.New(plan.PathFor(t.TempDir(), "sess"))
	e, err := NewExitPlanMode(store)
	if err != nil {
		t.Fatal(err)
	}
	return e, store
}

func TestExitPlanApproved(t *testing.T) {
	e, store := newExitPlan(t)
	in, _ := json.Marshal(ExitPlanModeInput{Plan: "1. do X\n2. do Y"})
	p := &fakePlanner{approved: true}
	res, _ := e.Execute(context.Background(), Context{Plan: p}, in)
	if p.gotPlan == "" || res[0].IsError || !strings.Contains(res[0].Content, "approved") {
		t.Errorf("approved path: plan=%q res=%+v", p.gotPlan, res[0])
	}
	// An approved plan is multi-step work the user has blessed, so it stops
	// being prose in a message history that compaction can summarise away.
	if !store.Approved() {
		t.Error("approval was not recorded in the store")
	}
	body, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("the approved plan was not written: %v", err)
	}
	if !strings.Contains(string(body), "do X") {
		t.Errorf("artefact does not contain the plan:\n%s", body)
	}
	// The user asked for approval to start work on a visible list, not just to
	// unlock the tools.
	if !strings.Contains(res[0].Content, "TodoWrite") {
		t.Errorf("approval does not tell the model to track the steps: %q", res[0].Content)
	}
}

func TestExitPlanRejected(t *testing.T) {
	e, store := newExitPlan(t)
	in, _ := json.Marshal(ExitPlanModeInput{Plan: "do X"})
	res, _ := e.Execute(context.Background(), Context{Plan: &fakePlanner{approved: false}}, in)
	if !strings.Contains(res[0].Content, "did not approve") {
		t.Errorf("rejected path: %+v", res[0])
	}
	// Nothing is persisted for a plan the user turned down.
	if store.Approved() {
		t.Error("a rejected plan was recorded as approved")
	}
	if _, err := os.Stat(store.Path()); err == nil {
		t.Error("a rejected plan was written to disk")
	}
}

func TestExitPlanHeadless(t *testing.T) {
	e, _ := newExitPlan(t)
	in, _ := json.Marshal(ExitPlanModeInput{Plan: "the plan body"})
	res, _ := e.Execute(context.Background(), Context{Plan: nil}, in)
	if !strings.Contains(res[0].Content, "the plan body") {
		t.Errorf("headless should surface the plan: %+v", res[0])
	}
}

// Approving a plan with its steps makes approval and the checklist one act;
// without steps the previous plan's list is cleared rather than kept.
func TestExitPlanStartsTheChecklist(t *testing.T) {
	tests := []struct {
		name     string
		steps    []string
		want     []string
		mentions string
	}{
		{name: "steps given", steps: []string{"build it", "ship it"}, want: []string{"build it", "ship it"}, mentions: "Its 2 steps"},
		{name: "no steps", mentions: "Record the plan's steps with TodoWrite"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, store := newExitPlan(t)
			_ = store.Replace([]plan.Item{{Content: "research step", Status: plan.StatusInProgress}})
			in, _ := json.Marshal(ExitPlanModeInput{Plan: "Ship it.", Steps: tc.steps})
			res, _ := e.Execute(context.Background(), Context{Plan: &fakePlanner{approved: true}}, in)
			var got []string
			for _, it := range store.Items() {
				got = append(got, it.Content)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("checklist = %q, want %q", got, tc.want)
			}
			if !strings.Contains(res[0].Content, tc.mentions) || !strings.Contains(res[0].Content, "the list is what is followed") {
				t.Errorf("result = %q", res[0].Content)
			}
		})
	}
}
