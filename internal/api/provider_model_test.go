package api

import (
	"strings"
	"testing"
)

// Claude's aliases, context windows and output caps describe Anthropic's
// models. With provider = "openai" "sonnet" used to reach the endpoint as
// claude-sonnet-5 with max_tokens 128000 (#125).
func TestResolveModelForResolvesAliasesOnlyOnAnthropic(t *testing.T) {
	cases := []struct {
		provider, in, want string
	}{
		{"", "sonnet", "claude-sonnet-5"},
		{"anthropic", "Opus", "claude-opus-5"},
		{"Anthropic", "", DefaultModel},
		{"openai", "sonnet", "sonnet"},
		{"openai", " openai/gpt-5.5 ", "openai/gpt-5.5"},
		{"openai", "claude-sonnet-5", "claude-sonnet-5"},
		// No Claude default for another provider: the caller must say.
		{"openai", "", ""},
	}
	for _, tc := range cases {
		if got := ResolveModelFor(tc.provider, tc.in); string(got) != tc.want {
			t.Errorf("ResolveModelFor(%q, %q) = %q, want %q", tc.provider, tc.in, got, tc.want)
		}
	}
}

func TestMaxOutputTokensForIgnoresClaudeTableOffAnthropic(t *testing.T) {
	if got := MaxOutputTokensFor("", "sonnet"); got != 128000 {
		t.Errorf("anthropic sonnet = %d, want 128000", got)
	}
	for _, model := range []string{"sonnet", "claude-sonnet-5", "gpt-oss-120b"} {
		if got := MaxOutputTokensFor("openai", model); got != DefaultMaxOutputTokens {
			t.Errorf("openai %q = %d, want the unknown-model default %d", model, got, DefaultMaxOutputTokens)
		}
	}
}

func TestContextWindowForIgnoresClaudeTableOffAnthropic(t *testing.T) {
	if limit, source := ContextWindowFor("anthropic", "sonnet", 0); limit != 1_000_000 || source != ContextSourceModel {
		t.Errorf("anthropic sonnet = (%d, %q), want the table's 1M", limit, source)
	}
	if limit, source := ContextWindowFor("openai", "claude-sonnet-5", 0); limit != 0 || source != ContextSourceUnknown {
		t.Errorf("openai claude-sonnet-5 = (%d, %q), want unknown", limit, source)
	}
	// The explicit override is the OpenAI escape hatch and must still win.
	if limit, source := ContextWindowFor("openai", "sonnet", 128000); limit != 128000 || source != ContextSourceConfig {
		t.Errorf("openai with override = (%d, %q), want (128000, config)", limit, source)
	}
}

func TestAliasWarning(t *testing.T) {
	for _, alias := range []string{"opus", "sonnet", "haiku", "fable", " Sonnet "} {
		w := AliasWarning("openai", alias)
		if w == "" {
			t.Errorf("no warning for alias %q on openai", alias)
			continue
		}
		if !strings.Contains(w, strings.TrimSpace(alias)) || !strings.Contains(w, "Anthropic") {
			t.Errorf("warning for %q should name the alias and the provider it needs: %q", alias, w)
		}
	}
	for _, tc := range []struct{ provider, model string }{
		{"", "sonnet"},
		{"anthropic", "opus"},
		{"openai", "claude-sonnet-5"},
		{"openai", "openai/gpt-5.5"},
		{"openai", ""},
	} {
		if w := AliasWarning(tc.provider, tc.model); w != "" {
			t.Errorf("AliasWarning(%q, %q) = %q, want none", tc.provider, tc.model, w)
		}
	}
}
