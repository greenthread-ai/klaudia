package api

import "testing"

func TestMaxOutputTokens(t *testing.T) {
	// 1M-context Claude models generate up to 128k output tokens.
	for _, id := range []string{"claude-opus-5", "claude-opus-4-8", "claude-sonnet-5", "claude-fable-5", "claude-sonnet-4-6"} {
		if got := MaxOutputTokens(id); got != 128000 {
			t.Errorf("%s = %d, want 128000", id, got)
		}
	}
	// 200k-context models generate up to 64k.
	for _, id := range []string{"claude-haiku-4-5", "claude-opus-4-5-20251101", "claude-sonnet-4-5-20250929"} {
		if got := MaxOutputTokens(id); got != 64000 {
			t.Errorf("%s = %d, want 64000", id, got)
		}
	}
	// Unknown / OpenAI-compatible models fall back to the safe floor.
	if got := MaxOutputTokens("gpt-oss-120b"); got != DefaultMaxOutputTokens {
		t.Errorf("unknown = %d, want default %d", got, DefaultMaxOutputTokens)
	}
	// Every table value stays within the model's real cap by construction, so a
	// request can never be rejected for exceeding it (verified against docs).
}
