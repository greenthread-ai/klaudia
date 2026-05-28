package api

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestResolveModelAliases(t *testing.T) {
	// CLI aliases must point at the *current* Claude 4.x lineup so `--model opus`
	// matches what Claude Code itself uses on Claude Max (Opus 4.7), not a
	// stale generation. Full model IDs pass through unchanged.
	tests := []struct {
		in   string
		want anthropic.Model
	}{
		{"", DefaultModel},
		{"  ", DefaultModel},
		{"opus", "claude-opus-4-7"},
		{"sonnet", "claude-sonnet-4-6"},
		{"haiku", "claude-haiku-4-5"},
		{"OPUS", "claude-opus-4-7"},                    // case-insensitive
		{"claude-opus-4-7", "claude-opus-4-7"},         // explicit ID passes through
		{"openai/gpt-oss-120b", "openai/gpt-oss-120b"}, // foreign IDs untouched
	}
	for _, tc := range tests {
		if got := ResolveModel(tc.in); got != tc.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
