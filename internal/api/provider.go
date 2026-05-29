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
		// Defend against an SDK bug in Accumulate: BetaRawContentBlockStopEvent
		// and BetaRawMessageStopEvent both run `json.Marshal(content_block)` /
		// `json.Marshal(acc)` to refresh the JSON.raw cache. If any content
		// block's Input (a json.RawMessage) ended up zero-length — observed
		// when a tool_use start event arrives with Input: null and no
		// input_json_delta follows — the marshal fails with "unexpected end of
		// JSON input" and the whole stream aborts even though we have a
		// usable accumulated message. Losing 4M-token /goal iterations to one
		// malformed event isn't OK, so we patch empty Input fields to "{}" —
		// semantically the correct empty-object placeholder — before each
		// stop event that triggers a marshal.
		repairEmptyToolInputs(&acc, ev)
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

// repairEmptyToolInputs replaces empty-but-non-nil Input json.RawMessage values
// in acc.Content with []byte("{}") just before the SDK runs json.Marshal on
// the containing block(s). Only fires on the two stop events that trigger
// marshaling — message_stop (marshals acc) and content_block_stop (marshals
// the most recent block) — so normal accumulation isn't touched.
//
// Nil RawMessages are left alone on purpose — `RawMessage(nil).MarshalJSON()`
// returns "null" cleanly, so the bug is specifically `[]byte{}` (length 0,
// non-nil). Touching nil would convert "field absent" to "field present but
// empty" semantically, which the SDK shouldn't see from us.
//
// Safe when acc.Content is empty (the loop does nothing).
func repairEmptyToolInputs(acc *anthropic.BetaMessage, ev anthropic.BetaRawMessageStreamEventUnion) {
	patch := func(cb *anthropic.BetaContentBlockUnion) {
		if cb.Input != nil && len(cb.Input) == 0 {
			cb.Input = []byte("{}")
		}
	}
	switch ev.AsAny().(type) {
	case anthropic.BetaRawMessageStopEvent:
		for i := range acc.Content {
			patch(&acc.Content[i])
		}
	case anthropic.BetaRawContentBlockStopEvent:
		if n := len(acc.Content); n > 0 {
			patch(&acc.Content[n-1])
		}
	}
}
