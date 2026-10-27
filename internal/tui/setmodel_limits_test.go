package tui

import (
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/api"
)

// /model sized the new model on its own: provider figures over the table, and
// no config at all, so a maxTokens set in .klaudia/config.toml stopped
// applying after any switch. It must use the session's resolver — the one
// startup uses, with config bound in.
func TestModelSwitchKeepsTheConfigOverride(t *testing.T) {
	m := newTestModel()
	var gotKnown api.ModelInfo
	m.sess.ResolveLimits = func(model string, known api.ModelInfo) api.ModelLimits {
		gotKnown = known
		return api.Resolve(t.Context(), api.LimitsInput{
			Model: model, Known: known, NoNetwork: true, ConfigMaxTokens: 4096,
		})
	}

	// The picker's provider figures for the chosen model: 64k out, 200k window.
	m.setModel("claude-opus-5-5", 200_000, 64000)

	if m.sess.MaxTokens != 4096 || m.sess.MaxTokensSource != api.LimitSourceConfig {
		t.Errorf("max tokens = (%d, %q), want the config override (4096, %q)",
			m.sess.MaxTokens, m.sess.MaxTokensSource, api.LimitSourceConfig)
	}
	if m.sess.ContextWindow != 200_000 || m.sess.ContextWindowSource != api.LimitSourceProvider {
		t.Errorf("context = (%d, %q), want the provider's (200000, %q)",
			m.sess.ContextWindow, m.sess.ContextWindowSource, api.LimitSourceProvider)
	}
	if gotKnown.MaxTokens != 64000 || gotKnown.ContextWindow != 200_000 {
		t.Errorf("the resolver was not given the picker's figures: %+v", gotKnown)
	}
}
