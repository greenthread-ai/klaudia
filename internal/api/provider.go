package api

import (
	"context"
	"encoding/json"

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
		// Defend against an SDK bug in Accumulate (upstream issue #292):
		// BetaRawContentBlockStopEvent and BetaRawMessageStopEvent run
		// `json.Marshal(content_block)` / `json.Marshal(acc)` to refresh the
		// JSON.raw cache. The encoder validates each json.RawMessage via
		// json.Compact, so any non-valid Input — zero-length OR truncated
		// (e.g. `{"argument":` when max_tokens cuts off mid tool_use) —
		// fails with "unexpected end of JSON input" and the whole stream
		// aborts even though we have a usable accumulated message. Losing
		// long /goal iterations to one malformed event isn't OK, so we patch
		// invalid Input to "{}" before each stop event that triggers a
		// marshal. A previous upstream attempt (PR #307) was withdrawn by its
		// author with uncertainty; ours is narrower (only the stop-event
		// marshal paths, only invalid Input fields).
		repairInvalidToolInputs(&acc, ev)
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

// repairInvalidToolInputs replaces any json.RawMessage Input that wouldn't
// round-trip through json.Marshal with []byte("{}"), just before the SDK
// triggers a marshal on the containing block(s). Catches three real shapes
// of the bug confirmed against Go's encoder:
//
//   - empty non-nil ([]byte{}) — from `"input": ""` in a start event
//   - truncated (e.g. `{"argument":`) — from max_tokens hitting mid tool_use
//   - unclosed-string (e.g. `{"x": "abc`) — same root cause
//
// All three produce the same "unexpected end of JSON input" error when the
// encoder validates the RawMessage via json.Compact. Nil RawMessages are left
// alone — they marshal as "null" cleanly, and treating "field absent" as
// "field present but empty" would be a behaviour change.
//
// Only fires on message_stop and content_block_stop — the events that
// actually trigger marshaling inside Accumulate — so normal in-flight
// accumulation (where Input legitimately mutates over input_json_delta
// events) is untouched. Patching to "{}" rather than dropping the block
// keeps the tool_use structurally valid; the agent then dispatches with
// empty args, which most tools surface as a recoverable validation error
// the model can fix on the next turn.
func repairInvalidToolInputs(acc *anthropic.BetaMessage, ev anthropic.BetaRawMessageStreamEventUnion) {
	patch := func(cb *anthropic.BetaContentBlockUnion) {
		if cb.Input != nil && !json.Valid(cb.Input) {
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
