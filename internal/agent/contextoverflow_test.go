package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// overflowThenOKProvider rejects the first request the way Anthropic does when
// the conversation is larger than the window, then succeeds.
type overflowThenOKProvider struct {
	calls int
	sizes []int // messages per request, to prove the retry was smaller
}

func (p *overflowThenOKProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.calls++
	p.sizes = append(p.sizes, len(params.Messages))
	if p.calls == 1 {
		return anthropic.BetaMessage{}, errors.New("prompt is too long: 1000464 tokens > 1000000 maximum")
	}
	return anthropic.BetaMessage{
		StopReason: "end_turn",
		Content: []anthropic.BetaContentBlockUnion{{
			Type: "text", Text: "recovered",
		}},
	}, nil
}

// Reported failure: a four-day session crossed the window, and because every
// resend is the same oversized request, the session was finished. The loop must
// summarise and retry once instead of returning the 400.
func TestContextOverflowIsRecovered(t *testing.T) {
	provider := &overflowThenOKProvider{}
	loop := New(provider, tools.NewRegistry())

	var events []string
	res, err := loop.Run(context.Background(), Options{
		Prompt:        "carry on with the work",
		ContextWindow: 1_000_000,
	}, func(e Event) {
		if e.Type == "compaction" && e.Content != "" {
			events = append(events, e.Content)
		}
	})
	if err != nil {
		t.Fatalf("overflow should be recovered, got %v", err)
	}
	// Three provider calls, not two: the rejected turn, the summarisation
	// request that autocompact makes, then the retry.
	if provider.calls != 3 {
		t.Fatalf("provider called %d times, want 3 (reject, summarise, retry)", provider.calls)
	}
	// The retry must be a smaller conversation than the one that was refused,
	// or the recovery is cosmetic.
	if provider.sizes[2] >= provider.sizes[0]+2 {
		t.Errorf("retry carried %d messages vs %d originally — compaction did nothing",
			provider.sizes[2], provider.sizes[0])
	}
	// finalText comes from the stream sink, which a scripted provider never
	// feeds; the turn completing normally is the signal that matters here.
	if res.StopReason != "end_turn" {
		t.Errorf("stop reason = %q, want end_turn from the retried turn", res.StopReason)
	}
	var told bool
	for _, e := range events {
		if strings.Contains(e, "overflow") {
			told = true
		}
	}
	if !told {
		t.Errorf("the user must be told why the turn was summarised, got %q", events)
	}
}

// A second overflow is a real failure: retrying forever would hide it.
type alwaysOverflowProvider struct{ calls int }

func (p *alwaysOverflowProvider) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.calls++
	return anthropic.BetaMessage{}, errors.New("prompt is too long: 1000464 tokens > 1000000 maximum")
}

func TestPersistentOverflowStillFails(t *testing.T) {
	provider := &alwaysOverflowProvider{}
	loop := New(provider, tools.NewRegistry())

	_, err := loop.Run(context.Background(), Options{Prompt: "hi", ContextWindow: 1_000_000}, nil)
	if err == nil {
		t.Fatal("a conversation that overflows after compaction must surface the error")
	}
	if !api.IsContextOverflow(err) {
		t.Errorf("err = %v, want the overflow error", err)
	}
	// Reject, summarise, retry — and then stop, rather than looping forever
	// on a conversation that is over the limit even after summarising.
	if provider.calls != 3 {
		t.Errorf("provider called %d times, want 3 (reject, summarise, retry once)", provider.calls)
	}
}
