package api

import (
	"context"
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// fakeLister is a Provider that can enumerate models, and counts how often it
// is asked — the point of ResolveLimits is that a known model costs no call.
type fakeLister struct {
	models []ModelInfo
	err    error
	calls  int
}

func (f *fakeLister) StreamTurn(context.Context, anthropic.BetaMessageNewParams, StreamSink) (anthropic.BetaMessage, error) {
	return anthropic.BetaMessage{}, errors.New("not used")
}

func (f *fakeLister) ListModels(context.Context) ([]ModelInfo, error) {
	f.calls++
	return f.models, f.err
}

// mute is a Provider with no model enumeration at all.
type mute struct{}

func (mute) StreamTurn(context.Context, anthropic.BetaMessageNewParams, StreamSink) (anthropic.BetaMessage, error) {
	return anthropic.BetaMessage{}, errors.New("not used")
}

func TestResolveLimits(t *testing.T) {
	live := []ModelInfo{
		{ID: "claude-opus-9-9", MaxTokens: 256000, ContextWindow: 2_000_000},
		{ID: "gpt-5.5", MaxTokens: 0, ContextWindow: 0},
	}
	tests := []struct {
		name          string
		model         string
		cfgMaxTokens  int
		cfgContext    int
		provider      Provider
		wantMaxTokens int
		wantMaxSource string
		wantContext   int
		wantCtxSource string
		wantCalls     int
	}{
		{
			name: "a known model is answered offline, with no provider call",
			// The whole fast path: the table already has the right number, so
			// asking the endpoint would add latency to every run for nothing.
			model:         "claude-opus-5-5",
			provider:      &fakeLister{models: live},
			wantMaxTokens: 128000,
			wantMaxSource: LimitSourceModel,
			wantContext:   1_000_000,
			wantCtxSource: LimitSourceModel,
			wantCalls:     0,
		},
		{
			name:          "an alias resolves before the lookup",
			model:         "opus",
			provider:      &fakeLister{models: live},
			wantMaxTokens: 128000,
			wantMaxSource: LimitSourceModel,
			wantContext:   1_000_000,
			wantCtxSource: LimitSourceModel,
			wantCalls:     0,
		},
		{
			name: "an unknown model takes the provider's figures, not the floor",
			// This is the Opus 5.5 defect: before, a model the table had never
			// heard of silently got 8192 instead of its real cap.
			model:         "claude-opus-9-9",
			provider:      &fakeLister{models: live},
			wantMaxTokens: 256000,
			wantMaxSource: LimitSourceProvider,
			wantContext:   2_000_000,
			wantCtxSource: LimitSourceProvider,
			wantCalls:     1,
		},
		{
			name:          "config beats the provider's own answer",
			model:         "claude-opus-9-9",
			cfgMaxTokens:  32000,
			cfgContext:    500_000,
			provider:      &fakeLister{models: live},
			wantMaxTokens: 32000,
			wantMaxSource: LimitSourceConfig,
			wantContext:   500_000,
			wantCtxSource: LimitSourceConfig,
			wantCalls:     1,
		},
		{
			name:          "config beats the table too",
			model:         "claude-opus-5-5",
			cfgMaxTokens:  4096,
			provider:      &fakeLister{models: live},
			wantMaxTokens: 4096,
			wantMaxSource: LimitSourceConfig,
			wantContext:   1_000_000,
			wantCtxSource: LimitSourceModel,
			wantCalls:     0,
		},
		{
			name: "an endpoint that reports no sizes leaves the floor and unknown",
			// OpenAI-compatible servers list ids and nothing else. The output
			// cap must be a number, so it floors; the window stays unknown
			// rather than being guessed high and skipping compaction.
			model:         "gpt-5.5",
			provider:      &fakeLister{models: live},
			wantMaxTokens: DefaultMaxOutputTokens,
			wantMaxSource: LimitSourceFallback,
			wantContext:   0,
			wantCtxSource: ContextSourceUnknown,
			wantCalls:     1,
		},
		{
			name:          "a vendor-prefixed id matches the listed bare id",
			model:         "openai/gpt-5.5",
			provider:      &fakeLister{models: []ModelInfo{{ID: "gpt-5.5", MaxTokens: 32000}}},
			wantMaxTokens: 32000,
			wantMaxSource: LimitSourceProvider,
			wantContext:   0,
			wantCtxSource: ContextSourceUnknown,
			wantCalls:     1,
		},
		{
			name:          "a provider that cannot list models falls back silently",
			model:         "some-local-model",
			provider:      mute{},
			wantMaxTokens: DefaultMaxOutputTokens,
			wantMaxSource: LimitSourceFallback,
			wantContext:   0,
			wantCtxSource: ContextSourceUnknown,
			wantCalls:     0,
		},
		{
			name:          "a failing lookup falls back rather than failing the run",
			model:         "some-local-model",
			provider:      &fakeLister{err: errors.New("connection refused")},
			wantMaxTokens: DefaultMaxOutputTokens,
			wantMaxSource: LimitSourceFallback,
			wantContext:   0,
			wantCtxSource: ContextSourceUnknown,
			wantCalls:     1,
		},
		{
			name:          "a nil provider is fine",
			model:         "claude-opus-5-5",
			provider:      nil,
			wantMaxTokens: 128000,
			wantMaxSource: LimitSourceModel,
			wantContext:   1_000_000,
			wantCtxSource: LimitSourceModel,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveLimits(context.Background(), tc.provider, tc.model, tc.cfgMaxTokens, tc.cfgContext)
			if got.MaxOutputTokens != tc.wantMaxTokens || got.MaxOutputSource != tc.wantMaxSource {
				t.Errorf("output = (%d, %q), want (%d, %q)", got.MaxOutputTokens, got.MaxOutputSource, tc.wantMaxTokens, tc.wantMaxSource)
			}
			if got.ContextWindow != tc.wantContext || got.ContextSource != tc.wantCtxSource {
				t.Errorf("context = (%d, %q), want (%d, %q)", got.ContextWindow, got.ContextSource, tc.wantContext, tc.wantCtxSource)
			}
			if f, ok := tc.provider.(*fakeLister); ok && f.calls != tc.wantCalls {
				t.Errorf("provider asked %d times, want %d", f.calls, tc.wantCalls)
			}
		})
	}
}
