package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
)

// overCapError is the 400 a provider returns when max_tokens is above the
// model's output cap, naming the real limit the way the measured Anthropic
// message does (see api.MaxTokensTooLarge).
func overCapError(asked, limit int) error {
	body, err := json.Marshal(map[string]any{"error": map[string]string{
		"type": "invalid_request_error",
		"message": "max_tokens: " + strconv.Itoa(asked) + " > " + strconv.Itoa(limit) +
			", which is the maximum allowed number of output tokens for this-model",
	}})
	if err != nil {
		panic(err)
	}
	return &api.OpenAIError{StatusCode: 400, Body: string(body)}
}

// overCapProvider refuses any request asking for more than limit output tokens
// and records what each request asked for, so a test can prove the retry sent a
// different number rather than the same one.
type overCapProvider struct {
	limit int
	asked []int64
}

func (p *overCapProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.asked = append(p.asked, params.MaxTokens)
	if params.MaxTokens > int64(p.limit) {
		return anthropic.BetaMessage{}, overCapError(int(params.MaxTokens), p.limit)
	}
	return anthropic.BetaMessage{
		StopReason: "end_turn",
		Content:    []anthropic.BetaContentBlockUnion{{Type: "text", Text: "recovered"}},
	}, nil
}

// The cap we send comes from an offline table (api.modelMaxOutputTokens) or the
// user's config, and when it is too high every resend fails identically — the
// session is over. The 400 names the model's real limit, so the loop adopts it
// and retakes the turn.
func TestOverCapMaxTokensIsLoweredAndRetried(t *testing.T) {
	provider := &overCapProvider{limit: 64000}
	loop := New(provider, tools.NewRegistry())

	var notices []string
	res, err := loop.Run(context.Background(), Options{
		Prompt:    "write the file",
		MaxTokens: 128000,
	}, func(e Event) {
		if e.Type == "notice" {
			notices = append(notices, e.Content)
		}
	})
	if err != nil {
		t.Fatalf("an over-cap max_tokens should be recovered, got %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Errorf("stop reason = %q, want end_turn from the retried turn", res.StopReason)
	}
	if want := []int64{128000, 64000}; len(provider.asked) != len(want) ||
		provider.asked[0] != want[0] || provider.asked[1] != want[1] {
		t.Fatalf("requests asked for %v tokens, want %v (refused, then retried at the real cap)", provider.asked, want)
	}
	// Silent recovery would leave the user with a model writing less per turn
	// than the table claims and no way to know the table is wrong.
	var told bool
	for _, n := range notices {
		if strings.Contains(n, "64000") {
			told = true
		}
	}
	if !told {
		t.Errorf("the user must be told the real limit, got %q", notices)
	}
}

// shrinkingCapProvider refuses every request and names a lower limit each time:
// the shape of a provider whose error does not actually explain the refusal.
// Retrying on each new number would loop until MaxTurns.
type shrinkingCapProvider struct {
	limits []int // the cap named by each successive refusal
	calls  int
}

func (p *shrinkingCapProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	limit := p.limits[min(p.calls, len(p.limits)-1)]
	p.calls++
	return anthropic.BetaMessage{}, overCapError(int(params.MaxTokens), limit)
}

func TestRepeatedOverCapRefusalSurfaces(t *testing.T) {
	provider := &shrinkingCapProvider{limits: []int{64000, 32000, 16000}}
	loop := New(provider, tools.NewRegistry())

	_, err := loop.Run(context.Background(), Options{Prompt: "hi", MaxTokens: 128000}, nil)
	if err == nil {
		t.Fatal("a request still refused after lowering the cap must surface the error")
	}
	if _, ok := api.MaxTokensTooLarge(err); !ok {
		t.Errorf("err = %v, want the over-cap 400", err)
	}
	// Refuse, lower, refuse — and then stop. The lowering happens once per Run
	// because a second refusal means the number in the error is not the number
	// being rejected.
	if provider.calls != 2 {
		t.Errorf("provider called %d times, want 2 (refused, retried once at the named cap)", provider.calls)
	}
}
