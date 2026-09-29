package agent

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestSubagentModel(t *testing.T) {
	for _, tc := range []struct{ parent, own, want string }{
		{"claude-opus-5", "", "claude-opus-5"},
		{"claude-opus-5", "sonnet", "claude-sonnet-5"},
		{"claude-opus-5", "claude-haiku-4-5", "claude-haiku-4-5"},
		// A non-Claude session is not moved onto a Claude id it cannot serve.
		{"openai/gpt-5.5", "sonnet", "openai/gpt-5.5"},
		{"openai/gpt-5.5", "openai/gpt-5.5-mini", "openai/gpt-5.5-mini"},
	} {
		if got := string(subagentModel(anthropicModel(tc.parent), tc.own)); got != tc.want {
			t.Errorf("subagentModel(%q, %q) = %q, want %q", tc.parent, tc.own, got, tc.want)
		}
	}
}

func anthropicModel(s string) anthropic.Model { return anthropic.Model(s) }
