package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeSpawner struct {
	gotType, gotPrompt string
	result             string
	gotProgress        func(string) // captured so a test can assert it was forwarded
}

func (f *fakeSpawner) Spawn(_ context.Context, subagentType, prompt string, progress func(string)) (string, error) {
	f.gotType, f.gotPrompt, f.gotProgress = subagentType, prompt, progress
	if progress != nil {
		progress("Read main.go") // a child tool call, as the real spawner relays
	}
	return f.result, nil
}

func newTestAgent(t *testing.T, sp Spawner) *Agent {
	t.Helper()
	a, err := NewAgent(sp, []AgentTypeInfo{
		{Name: "general-purpose", Description: "all tools"},
		{Name: "Explore", Description: "read only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAgentValidateUnknownType(t *testing.T) {
	a := newTestAgent(t, &fakeSpawner{})
	raw, _ := json.Marshal(AgentInput{Prompt: "x", SubagentType: "bogus", Description: "d"})
	if err := a.ValidateInput(raw); err == nil {
		t.Error("expected unknown subagent_type to be rejected")
	}
	raw, _ = json.Marshal(AgentInput{Prompt: "x", SubagentType: "Explore", Description: "d"})
	if err := a.ValidateInput(raw); err != nil {
		t.Errorf("valid type rejected: %v", err)
	}
}

func TestAgentExecuteDelegatesToSpawner(t *testing.T) {
	sp := &fakeSpawner{result: "sub-agent findings"}
	a := newTestAgent(t, sp)
	raw, _ := json.Marshal(AgentInput{Prompt: "find the bug", SubagentType: "Explore", Description: "d"})
	res, err := a.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if sp.gotType != "Explore" || sp.gotPrompt != "find the bug" {
		t.Errorf("spawner got type=%q prompt=%q", sp.gotType, sp.gotPrompt)
	}
	if res[0].Content != "sub-agent findings" {
		t.Errorf("result = %q", res[0].Content)
	}
}

func TestAgentDescriptionListsTypes(t *testing.T) {
	a := newTestAgent(t, &fakeSpawner{})
	desc, _ := a.Description(context.Background())
	if !contains(desc, "general-purpose") || !contains(desc, "Explore") {
		t.Errorf("description missing types: %s", desc)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type fakeRunner struct {
	label string
	work  func(context.Context, func(string)) (string, error)
}

func (f *fakeRunner) Start(label string, work func(context.Context, func(string)) (string, error)) string {
	f.label, f.work = label, work
	return "agent_7"
}

// run_in_background hands the spawn to the host's runner and returns at once;
// without a runner the sub-agent runs in the foreground and says so.
func TestAgentRunInBackground(t *testing.T) {
	sp := &countingSpawner{result: "child result"}
	a, err := NewAgent(sp, []AgentTypeInfo{{Name: "Explore"}})
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"description":"survey","prompt":"look","subagent_type":"Explore","run_in_background":true}`)

	runner := &fakeRunner{}
	res, err := a.Execute(context.Background(), Context{Background: runner}, raw)
	if err != nil || len(res) != 1 || !strings.Contains(res[0].Content, "agent_7") || sp.calls != 0 {
		t.Fatalf("background start: res=%+v err=%v spawns=%d", res, err, sp.calls)
	}
	if runner.label != "Explore: survey" {
		t.Errorf("label = %q", runner.label)
	}
	if got, _ := runner.work(context.Background(), nil); got != "child result" || sp.calls != 1 {
		t.Errorf("the runner's work should spawn the child: got %q, spawns %d", got, sp.calls)
	}

	res, _ = a.Execute(context.Background(), Context{}, raw)
	if !strings.Contains(res[0].Content, "ran in the foreground") || !strings.Contains(res[0].Content, "child result") {
		t.Errorf("no runner: %q", res[0].Content)
	}
}

type countingSpawner struct {
	result string
	calls  int
}

func (c *countingSpawner) Spawn(context.Context, string, string, func(string)) (string, error) {
	c.calls++
	return c.result, nil
}
