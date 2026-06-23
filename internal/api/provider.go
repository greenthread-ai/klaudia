package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

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

// defaultStreamIdleTimeout bounds how long the streamed turn may go without
// receiving any event before we treat the connection as dead. A stalled SSE
// connection (a proxy/NAT silently drops the flow, or the server stops sending
// after message_start) leaves the TCP socket "alive" while the read blocks
// forever — without this watchdog the agent loop parks indefinitely and the TUI
// just shows "thinking… quiet for <minutes>". 120s is comfortably longer than
// any legitimate gap between events for a model that's actively generating
// (extended-thinking deltas arrive well inside this window).
const defaultStreamIdleTimeout = 120 * time.Second

// streamIdleTimeout resolves the per-event idle deadline, overridable via
// KLAUDIA_STREAM_IDLE_TIMEOUT (seconds). A value <= 0 disables the watchdog.
func streamIdleTimeout() time.Duration {
	if v := os.Getenv("KLAUDIA_STREAM_IDLE_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return defaultStreamIdleTimeout
}

// maxStreamStallRetries bounds how many times StreamTurn transparently re-issues
// a request that stalled before delivering any output. Kept small: a stall that
// recurs immediately points at a real outage, which should surface as an error
// rather than loop silently.
const maxStreamStallRetries = 2

// ErrStreamStalled marks a turn aborted by the idle watchdog (no stream event
// for streamIdleTimeout) rather than a server-side timeout. FriendlyError keys
// off it to give network-stall advice instead of the generic baseURL hint.
var ErrStreamStalled = errors.New("model stream stalled")

// StreamTurn implements Provider for the native Anthropic client: it streams the
// Beta Messages API and accumulates the response. Default betas are applied
// when the caller set none. Each raw stream event is forwarded to the sink
// (for partial-message output) and text deltas are forwarded via OnText.
//
// An idle watchdog guards every attempt: if no stream event arrives within
// streamIdleTimeout the in-flight read is cancelled. When the stall happened
// before any output reached the caller, the whole request is re-issued
// transparently (up to maxStreamStallRetries); once output has been delivered a
// silent retry would duplicate it, so the stall surfaces as a DeadlineExceeded
// error instead. A genuine user interrupt cancels the caller's ctx and is
// always returned as-is — never retried.
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
	// Cache the stable prefix (tools + system) plus a rolling conversation
	// breakpoint so each turn doesn't re-pay full price for the whole history.
	applyCacheControl(&params)

	return c.streamRetrying(ctx, params, sink, streamIdleTimeout())
}

// streamRetrying runs streamOnce under the stall-retry policy. Split out from
// StreamTurn so tests can drive the watchdog with a sub-second idle window
// instead of the user-facing seconds-granularity default.
func (c *Client) streamRetrying(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink, idle time.Duration) (anthropic.BetaMessage, error) {
	for attempt := 0; ; attempt++ {
		acc, delivered, stalled, err := c.streamOnce(ctx, params, sink, idle)
		if !stalled {
			return acc, err
		}
		// The watchdog fired with the caller's ctx still live — a dead
		// connection, not a user interrupt. Retry only while nothing has been
		// handed to the sink yet; otherwise a fresh request would re-emit the
		// text/events already shown.
		if !delivered && attempt < maxStreamStallRetries {
			continue
		}
		return acc, fmt.Errorf("%w: no data for %s: %w", ErrStreamStalled, idle, err)
	}
}

// streamOnce runs a single streamed request under an idle watchdog. It returns
// the accumulated message, whether any output was delivered to the sink,
// whether the watchdog tripped (a stall distinct from a caller-driven cancel),
// and the terminal stream error. A non-positive idle disables the watchdog.
func (c *Client) streamOnce(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink, idle time.Duration) (acc anthropic.BetaMessage, delivered, stalled bool, err error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var tripped atomic.Bool
	var timer *time.Timer
	if idle > 0 {
		timer = time.AfterFunc(idle, func() {
			tripped.Store(true)
			cancel()
		})
		defer timer.Stop()
	}

	stream := c.sdk.Beta.Messages.NewStreaming(streamCtx, params)
	for stream.Next() {
		if timer != nil {
			// An event arrived — push the idle deadline out. A late firing that
			// races this Reset is harmless: tripped is already set and the
			// cancelled ctx ends the stream on the next read.
			timer.Reset(idle)
		}
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
		if aerr := acc.Accumulate(ev); aerr != nil {
			return acc, delivered, false, aerr
		}
		if sink.OnRawEvent != nil {
			delivered = true
			sink.raw(ev)
		}
		if d := ev.AsContentBlockDelta(); d.Delta.Text != "" {
			delivered = true
			sink.text(d.Delta.Text)
		}
	}
	// Distinguish an idle-watchdog cancel from a user interrupt: the watchdog
	// cancels only streamCtx, so the caller's ctx is still live when it trips.
	if tripped.Load() && ctx.Err() == nil {
		return acc, delivered, true, context.DeadlineExceeded
	}
	return acc, delivered, false, stream.Err()
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
