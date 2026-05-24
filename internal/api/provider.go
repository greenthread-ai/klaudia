package api

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

// Provider is the model backend abstraction. The agent loop speaks the
// canonical Anthropic message types; a Provider runs one streamed turn and
// returns the assembled assistant message. This lets non-Anthropic backends
// (e.g. an OpenAI-compatible Chat Completions endpoint) plug in via a
// translation shim without changing the loop, tools, sessions, or compaction.
type Provider interface {
	// StreamTurn issues one model call for the given (Anthropic-shaped) request,
	// invoking onText with each incremental assistant text delta, and returns
	// the fully assembled assistant message.
	StreamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, onText func(string)) (anthropic.BetaMessage, error)
}

// StreamTurn implements Provider for the native Anthropic client: it streams the
// Beta Messages API and accumulates the response. Default betas are applied
// when the caller set none.
func (c *Client) StreamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, onText func(string)) (anthropic.BetaMessage, error) {
	if len(params.Betas) == 0 {
		params.Betas = DefaultBetas
	}
	stream := c.sdk.Beta.Messages.NewStreaming(ctx, params)

	var acc anthropic.BetaMessage
	for stream.Next() {
		ev := stream.Current()
		if err := acc.Accumulate(ev); err != nil {
			return acc, err
		}
		if onText != nil {
			if d := ev.AsContentBlockDelta(); d.Delta.Text != "" {
				onText(d.Delta.Text)
			}
		}
	}
	return acc, stream.Err()
}
