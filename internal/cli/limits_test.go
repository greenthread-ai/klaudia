package cli

import (
	"testing"

	"github.com/greenthread-ai/klaudia/internal/api"
)

// The resolver /model gets must carry the config overrides; it is the reason
// /model and startup can no longer disagree about a configured cap.
func TestLimitsResolverBindsConfig(t *testing.T) {
	got := limitsResolver(nil, 4096, 300_000)("claude-opus-5-5", api.ModelInfo{MaxTokens: 64000, ContextWindow: 200_000})
	if got.MaxOutputTokens != 4096 || got.ContextWindow != 300_000 {
		t.Errorf("got (%d, %d), want the config overrides (4096, 300000)", got.MaxOutputTokens, got.ContextWindow)
	}
}
