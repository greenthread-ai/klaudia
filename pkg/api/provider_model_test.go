package api

import (
	"context"
	"strings"
	"testing"
)

// With provider = "openai", --model sonnet reached the endpoint as
// claude-sonnet-5 and was sized from the Claude tables. Aliases now expand
// only for Anthropic; no model at all still means DefaultModel everywhere.
// (PR #80.)
func TestAliasesExpandOnlyForAnthropic(t *testing.T) {
	for _, tc := range []struct{ provider, in, want string }{
		{"", "sonnet", string(ResolveModel("sonnet"))},
		{"anthropic", "opus", string(ResolveModel("opus"))},
		{"openai", "sonnet", "sonnet"},
		{"openai", "  gpt-4o ", "gpt-4o"},
		{"openai", "", DefaultModel},
	} {
		if got := string(ResolveModelFor(tc.provider, tc.in)); got != tc.want {
			t.Errorf("ResolveModelFor(%q, %q) = %q, want %q", tc.provider, tc.in, got, tc.want)
		}
	}
	if strings.HasPrefix(string(ResolveModel("sonnet")), "sonnet") {
		t.Fatal("test assumes sonnet is an alias")
	}
}

func TestAliasOnAnotherProviderIsNotSizedAsClaude(t *testing.T) {
	claude := Resolve(context.Background(), LimitsInput{Model: "sonnet", NoNetwork: true})
	other := Resolve(context.Background(), LimitsInput{Model: "sonnet", Provider: "openai", NoNetwork: true})
	if claude.MaxOutputTokens == DefaultMaxOutputTokens {
		t.Fatal("test assumes the table sizes sonnet above the floor")
	}
	if other.MaxOutputTokens != DefaultMaxOutputTokens || other.ContextWindow != 0 {
		t.Errorf("openai sonnet sized %d out / %d ctx, want the floor and unknown", other.MaxOutputTokens, other.ContextWindow)
	}
}

func TestAliasWarning(t *testing.T) {
	if w := AliasWarning("openai", "Sonnet"); !strings.Contains(w, "Claude alias") || !strings.Contains(w, string(ResolveModel("sonnet"))) {
		t.Errorf("openai alias warning = %q", w)
	}
	for _, tc := range [][2]string{{"", "sonnet"}, {"anthropic", "opus"}, {"openai", "gpt-4o"}, {"openai", ""}} {
		if w := AliasWarning(tc[0], tc[1]); w != "" {
			t.Errorf("AliasWarning(%q, %q) = %q, want none", tc[0], tc[1], w)
		}
	}
}
