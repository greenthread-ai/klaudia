package api

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestResolveModelAliases(t *testing.T) {
	// CLI aliases must point at the *current* lineup so `--model opus` gets the
	// current Opus rather than a stale generation. Full model IDs — including
	// older snapshots a user might want to pin — pass through unchanged.
	// Verified against GET /v1/models.
	tests := []struct {
		in   string
		want anthropic.Model
	}{
		{"", DefaultModel},
		{"  ", DefaultModel},
		{"opus", "claude-opus-5"},
		{"sonnet", "claude-sonnet-5"},
		{"haiku", "claude-haiku-4-5"},
		{"fable", "claude-fable-5"},
		{"OPUS", "claude-opus-5"},                                // case-insensitive
		{"claude-opus-4-8", "claude-opus-4-8"},                   // explicit current ID passes through
		{"claude-opus-4-7", "claude-opus-4-7"},                   // pinning an older snapshot still works
		{"claude-opus-4-5-20251101", "claude-opus-4-5-20251101"}, // dated snapshots pass through too
		{"openai/gpt-oss-120b", "openai/gpt-oss-120b"},           // foreign IDs untouched
	}
	for _, tc := range tests {
		if got := ResolveModel(tc.in); got != tc.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestContextWindowResolution(t *testing.T) {
	// Hybrid resolver for /stats and /doctor: positive config override always
	// wins (the OpenAI-compat escape hatch), then the per-model table, then a
	// fallback signalling we don't actually know.
	tests := []struct {
		name       string
		model      string
		override   int
		wantLimit  int
		wantSource string
	}{
		{"override wins over known model", "claude-opus-4-8", 8192, 8192, ContextSourceConfig},
		{"override wins over unknown model", "openai/gpt-oss-120b", 8192, 8192, ContextSourceConfig},
		{"alias resolves to current opus limit", "opus", 0, 1_000_000, ContextSourceModel},
		{"older opus snapshot still in the table", "claude-opus-4-7", 0, 1_000_000, ContextSourceModel},
		{"full id matches known limit", "claude-sonnet-4-6", 0, 1_000_000, ContextSourceModel},
		{"a 200K model is still reported as 200K", "claude-haiku-4-5", 0, 200_000, ContextSourceModel},
		{"unknown model, no override → unknown", "openai/gpt-oss-120b", 0, 0, ContextSourceUnknown},
		{"empty model + no override → DefaultModel's window", "", 0, 1_000_000, ContextSourceModel},
		{"negative override is ignored", "opus", -1, 1_000_000, ContextSourceModel},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			limit, source := ContextWindow(tc.model, tc.override)
			if limit != tc.wantLimit || source != tc.wantSource {
				t.Errorf("ContextWindow(%q, %d) = (%d, %q), want (%d, %q)", tc.model, tc.override, limit, source, tc.wantLimit, tc.wantSource)
			}
		})
	}
}
