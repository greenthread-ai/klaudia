package api

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"
)

// StreamSink receives streaming updates during one model turn. Both callbacks
// are optional and nil-safe (use the text/raw helpers). OnText carries
// incremental assistant text deltas — the TUI uses this for its single-reader
// rendering. OnRawEvent carries the raw Anthropic stream events, used to emit
// stream-json partial messages behind --include-partial-messages; the TUI
// leaves it nil so its single-reader invariant is untouched.
type StreamSink struct {
	OnText     func(string)
	OnRawEvent func(anthropic.BetaRawMessageStreamEventUnion)
}

func (s StreamSink) text(d string) {
	if s.OnText != nil {
		s.OnText(d)
	}
}

func (s StreamSink) raw(ev anthropic.BetaRawMessageStreamEventUnion) {
	if s.OnRawEvent != nil {
		s.OnRawEvent(ev)
	}
}

// Provider is the model backend abstraction. The agent loop speaks the
// canonical Anthropic message types; a Provider runs one streamed turn and
// returns the assembled assistant message. This lets non-Anthropic backends
// (e.g. an OpenAI-compatible Chat Completions endpoint) plug in via a
// translation shim without changing the loop, tools, sessions, or compaction.
type Provider interface {
	// StreamTurn issues one model call for the given (Anthropic-shaped) request,
	// forwarding incremental updates to sink, and returns the fully assembled
	// assistant message.
	StreamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink) (anthropic.BetaMessage, error)
}

// StreamTurn implements Provider for the native Anthropic client: it streams the
// Beta Messages API and accumulates the response. Default betas are applied
// when the caller set none. Each raw stream event is forwarded to the sink
// (for partial-message output) and text deltas are forwarded via OnText.
func (c *Client) StreamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink) (anthropic.BetaMessage, error) {
	if len(params.Betas) == 0 {
		params.Betas = DefaultBetas
	}
	// Credential-specific betas (e.g. oauth-2025-04-20) make our request match
	// what Claude Code itself sends with the same OAuth token, so the API doesn't
	// treat us as external usage and rate-limit aggressively.
	params.Betas = c.augmentBetas(params.Betas)
	// The OAuth bucket also requires an `x-anthropic-billing-header:` prefix
	// on the first system block — without it, every request 429s regardless of
	// headers. See augmentSystem for why.
	params.System = c.augmentSystem(params.System)
	stream := c.sdk.Beta.Messages.NewStreaming(ctx, params)

	var acc anthropic.BetaMessage
	for stream.Next() {
		ev := stream.Current()
		if err := acc.Accumulate(ev); err != nil {
			return acc, err
		}
		sink.raw(ev)
		if d := ev.AsContentBlockDelta(); d.Delta.Text != "" {
			sink.text(d.Delta.Text)
		}
	}
	return acc, stream.Err()
}
