package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakePlanner struct {
	gotPlan  string
	approved bool
}

func (f *fakePlanner) ExitPlan(_ context.Context, plan string) (bool, error) {
	f.gotPlan = plan
	return f.approved, nil
}

func newExitPlan(t *testing.T) *ExitPlanMode {
	t.Helper()
	e, err := NewExitPlanMode()
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExitPlanApproved(t *testing.T) {
	e := newExitPlan(t)
	in, _ := json.Marshal(ExitPlanModeInput{Plan: "1. do X\n2. do Y"})
	p := &fakePlanner{approved: true}
	res, _ := e.Execute(context.Background(), Context{Plan: p}, in)
	if p.gotPlan == "" || res[0].IsError || !strings.Contains(res[0].Content, "approved") {
		t.Errorf("approved path: plan=%q res=%+v", p.gotPlan, res[0])
	}
}

func TestExitPlanRejected(t *testing.T) {
	e := newExitPlan(t)
	in, _ := json.Marshal(ExitPlanModeInput{Plan: "do X"})
	res, _ := e.Execute(context.Background(), Context{Plan: &fakePlanner{approved: false}}, in)
	if !strings.Contains(res[0].Content, "did not approve") {
		t.Errorf("rejected path: %+v", res[0])
	}
}

func TestExitPlanHeadless(t *testing.T) {
	e := newExitPlan(t)
	in, _ := json.Marshal(ExitPlanModeInput{Plan: "the plan body"})
	res, _ := e.Execute(context.Background(), Context{Plan: nil}, in)
	if !strings.Contains(res[0].Content, "the plan body") {
		t.Errorf("headless should surface the plan: %+v", res[0])
	}
}
