package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type fakeSpawner struct {
	gotType, gotPrompt string
	result             string
	err                error
	gotProgress        func(string) // captured so a test can assert it was forwarded
}

func (f *fakeSpawner) Spawn(_ context.Context, subagentType, prompt string, progress func(string)) (string, error) {
	f.gotType, f.gotPrompt, f.gotProgress = subagentType, prompt, progress
	if progress != nil {
		progress("Read main.go") // a child tool call, as the real spawner relays
	}
	return f.result, f.err
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

// A failed sub-agent's partial work reaches the model with the error.
func TestAgentFailureCarriesPartialWork(t *testing.T) {
	a := newTestAgent(t, &fakeSpawner{result: "[partial] found the config", err: errors.New("overloaded")})
	res, err := a.Execute(context.Background(), Context{}, json.RawMessage(`{"subagent_type":"Explore","prompt":"p","description":"d"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].IsError || !strings.Contains(res[0].Content, "overloaded") || !strings.Contains(res[0].Content, "found the config") {
		t.Errorf("result = %+v, want the error and the partial work", res[0])
	}
}
