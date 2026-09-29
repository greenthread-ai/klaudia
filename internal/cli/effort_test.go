package cli

import (
	"testing"

	"github.com/greenthread-ai/klaudia/internal/config"
)

func TestResolveReasoningFlagWinsOverConfig(t *testing.T) {
	var warned []string
	warn := func(m string) { warned = append(warned, m) }
	effort, thinking, err := resolveReasoning("max", config.Config{Effort: "low", Thinking: "adaptive"}, warn)
	if err != nil || effort != "max" || thinking != "adaptive" || len(warned) != 0 {
		t.Errorf("got %q %q %v, warnings %v", effort, thinking, err, warned)
	}
	effort, _, _ = resolveReasoning("", config.Config{Effort: "Medium"}, warn)
	if effort != "medium" {
		t.Errorf("config effort not used: %q", effort)
	}
}

func TestResolveReasoningBadFlagIsUsageError(t *testing.T) {
	_, _, err := resolveReasoning("extreme", config.Config{}, func(string) {})
	if err == nil || exitCodeFor(err) != ExitUsage {
		t.Errorf("err = %v, want a usage error", err)
	}
}

func TestResolveReasoningBadConfigWarnsAndFallsBack(t *testing.T) {
	var warned []string
	effort, thinking, err := resolveReasoning("", config.Config{Effort: "loud", Thinking: "sometimes"},
		func(m string) { warned = append(warned, m) })
	if err != nil || effort != "" || thinking != "" {
		t.Errorf("got %q %q %v", effort, thinking, err)
	}
	if len(warned) != 2 {
		t.Errorf("warnings = %v, want one per bad setting", warned)
	}
}
