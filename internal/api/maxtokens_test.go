package api

import "testing"

func TestMaxOutputTokens(t *testing.T) {
	// Big Claude models get the raised cap.
	for _, id := range []string{"claude-opus-4-8", "claude-opus-5", "claude-sonnet-5"} {
		if got := MaxOutputTokens(id); got != 32000 {
			t.Errorf("%s = %d, want 32000", id, got)
		}
	}
	// Haiku and unknown models fall back to the conservative default.
	if got := MaxOutputTokens("claude-haiku-4-5"); got != DefaultMaxOutputTokens {
		t.Errorf("haiku = %d, want default %d", got, DefaultMaxOutputTokens)
	}
	if got := MaxOutputTokens("gpt-oss-120b"); got != DefaultMaxOutputTokens {
		t.Errorf("unknown = %d, want default %d", got, DefaultMaxOutputTokens)
	}
	// Never exceeds the model's real cap by construction (all table values are
	// under-approximations), and is always at least the safe floor.
	if MaxOutputTokens("claude-opus-4-8") < DefaultMaxOutputTokens {
		t.Error("raised cap should be >= the default floor")
	}
}
