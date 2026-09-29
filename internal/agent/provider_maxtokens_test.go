package agent

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// maxTokensProvider records the max_tokens of each request.
type maxTokensProvider struct{ got []int64 }

func (p *maxTokensProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.got = append(p.got, params.MaxTokens)
	return anthropic.BetaMessage{StopReason: "end_turn"}, nil
}

// The model-aware default is Claude's table on Anthropic only: a Claude id
// sent through an OpenAI-compatible endpoint gets the unknown-model cap (#125).
func TestMaxTokensDefaultFollowsProvider(t *testing.T) {
	cases := []struct {
		provider string
		want     int64
	}{
		{"", 128000},
		{"anthropic", 128000},
		{"openai", api.DefaultMaxOutputTokens},
	}
	for _, tc := range cases {
		p := &maxTokensProvider{}
		l := New(p, tools.NewRegistry())
		if _, err := l.Run(context.Background(), Options{
			Prompt:       "hi",
			Model:        "claude-sonnet-5",
			ProviderName: tc.provider,
			Permission:   bypassPerm(),
		}, nil); err != nil {
			t.Fatalf("%q: %v", tc.provider, err)
		}
		if len(p.got) != 1 || p.got[0] != tc.want {
			t.Errorf("provider %q: max_tokens = %v, want %d", tc.provider, p.got, tc.want)
		}
	}
}
