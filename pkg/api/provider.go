package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	// OnTransformations receives the response's input_transformations when
	// it lists any: thinking blocks the API dropped (thinkingbinding.go).
	OnTransformations func([]InputTransformation)
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

// maxStreamStallRetries bounds how many times StreamTurn transparently re-issues
// a request that stalled before delivering any output. Kept small: a stall that
// recurs immediately points at a real outage, which should surface as an error
// rather than loop silently.
const maxStreamStallRetries = 2

// ErrStreamStalled marks a turn aborted by the idle watchdog (no stream event
// for streamIdleTimeout) rather than a server-side timeout. FriendlyError keys
// off it to give network-stall advice instead of the generic baseURL hint.
var ErrStreamStalled = errors.New("model stream stalled")

// errStallMidStream distinguishes a stall that happened after output reached
// the caller — never retried, since a re-issue would duplicate what was shown —
// from one that stalled before any output and exhausted its retries.
var errStallMidStream = errors.New("after partial output")

// ErrStreamInterrupted marks a response the stream did not finish: it closed
// without message_stop, carried an overloaded or api error event after the
// response began, or could not be assembled. Like a stall, it is retried when
// nothing had been shown yet; after partial output it is reported as an
// interrupted response rather than passed off as a complete one.
var ErrStreamInterrupted = errors.New("model response interrupted")

// IsCutOff reports whether err ended a reply after part of it had been shown
// — a stall or an interruption mid-stream. Such a turn is not retried, and
// must not be handed to another model either: the user has read the start
// of this one.
func IsCutOff(err error) bool { return errors.Is(err, errStallMidStream) }

// errIncomplete is the interruption where the stream simply ended early.
var errIncomplete = errors.New("stream ended before message_stop")

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
	// The host's own shaping (WithPrepare), after the defaults and before
	// cache breakpoints, so whatever it adds is cached like the rest.
	if c.prepare != nil {
		c.prepare(&params)
	}
	// Preserved thinking: drop a mismatched block rather than fail the turn,
	// and report it (thinkingbinding.go). An endpoint that rejects the
	// controls themselves gets them no more for this client's life.
	// The same conversations get old tool results cleared server-side in
	// place of microcompact (contextedit.go), and every conversation has its
	// tool reveals sent as appended tool_addition messages (toolreveal.go).
	// An endpoint or model that rejects any of them is resent the turn
	// without it, and not asked again.
	base := params
	var added bool
	shape := func() anthropic.BetaMessageNewParams {
		p := base
		added = shapeReveals(&p, c.revealMode(p.Model))
		// Cache the stable prefix (tools + system) plus a rolling conversation
		// breakpoint so each turn doesn't re-pay full price for the whole
		// history. After the reveals, which add and remove messages.
		if !c.tuning.NoPromptCache {
			applyCacheControl(&p)
		}
		if !c.noBinding.Load() && historyHasThinking(p.Messages) {
			bindThinking(&p)
		}
		if c.ClearsToolResults(p.Messages) {
			clearToolResults(&p)
		}
		return p
	}

	msg, err := c.streamRetrying(ctx, shape(), sink, c.tuning.streamIdleTimeout())
	for retry := 0; err != nil && retry < 4; retry++ {
		switch {
		case !c.noBinding.Load() && isBindingRejected(err):
			c.noBinding.Store(true)
		case !c.noClearing.Load() && isClearingRejected(err):
			c.noClearing.Store(true)
		case !c.noDeferLoading.Load() && isDeferRejected(err):
			c.noDeferLoading.Store(true)
		case added && isToolChangeRejected(err):
			c.noToolChanges.Store(string(params.Model), true)
		default:
			retry = 4
			continue
		}
		msg, err = c.streamRetrying(ctx, shape(), sink, c.tuning.streamIdleTimeout())
	}
	return msg, annotateNotFound(err, string(params.Model), c.endpoint())
}

// streamRetrying runs streamOnce under the stall-retry policy. Split out from
// StreamTurn so tests can drive the watchdog with a sub-second idle window
// instead of the user-facing seconds-granularity default.
func (c *Client) streamRetrying(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink, idle time.Duration) (anthropic.BetaMessage, error) {
	for attempt := 0; ; attempt++ {
		acc, delivered, stalled, err := c.streamOnce(ctx, params, sink, idle)
		if !stalled && interrupted(ctx, err) {
			if !delivered && attempt < maxStreamStallRetries {
				continue
			}
			if delivered {
				return acc, fmt.Errorf("%w (%w): %w", ErrStreamInterrupted, errStallMidStream, err)
			}
			return acc, fmt.Errorf("%w: %w", ErrStreamInterrupted, err)
		}
		if !stalled {
			return acc, err
		}
		// The watchdog fired with the caller's ctx still live — a dead
		// connection, not a user interrupt. Retry only while nothing has been
		// handed to the sink yet; otherwise a fresh request would re-emit the
		// text/events already shown.
		if !delivered && attempt < maxStreamStallRetries {
			// The usual cause of a stall with nothing delivered is a pooled
			// connection that died while the session was idle. Retrying onto
			// the same corpse just burns another idle window.
			c.dropIdleConnections()
			continue
		}
		if delivered {
			return acc, fmt.Errorf("%w (%w): no data for %s: %w", ErrStreamStalled, errStallMidStream, idle, err)
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

	// Liveness is measured in bytes off the socket, not in decoded events: the
	// SDK drops the API's ping keepalives (`case "ping": continue` in
	// packages/ssestream), and those pings are exactly what a healthy stream
	// sends during the long pauses — a large tool_use payload, extended
	// thinking, a loaded service. Counting events instead killed live streams
	// that were merely slow. The tracker is touched by the round tripper.
	activity := newActivityTracker()
	streamCtx = withActivity(streamCtx, activity)

	var tripped atomic.Bool
	var timer *time.Timer
	if idle > 0 {
		// Re-arm rather than fire when bytes are still arriving, so the timer
		// only trips after a genuinely silent window.
		var arm func()
		arm = func() {
			if remaining := idle - activity.idleFor(); remaining > 0 {
				timer.Reset(remaining)
				return
			}
			tripped.Store(true)
			cancel()
		}
		timer = time.AfterFunc(idle, func() { arm() })
		defer timer.Stop()
	}

	stream := c.sdk.Beta.Messages.NewStreaming(streamCtx, params)
	sawStop := false
	for stream.Next() {
		ev := stream.Current()
		if ev.Type == "message_stop" {
			sawStop = true
		}
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
			return acc, delivered, false, fmt.Errorf("%w: %w", errAssemble, aerr)
		}
		if sink.OnTransformations != nil {
			if ts := transformationsOf(ev); len(ts) > 0 {
				resolveDropped(ts, params)
				sink.OnTransformations(ts)
			}
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
	if err := stream.Err(); err != nil {
		return acc, delivered, false, err
	}
	// A proxy or load balancer that closes the connection cleanly ends the
	// stream with no error, and the half-built message used to be returned as
	// the answer — a truncated reply, or a turn with no stop reason at all.
	if !sawStop && ctx.Err() == nil {
		return acc, delivered, false, errIncomplete
	}
	return acc, delivered, false, nil
}

// errAssemble wraps a failure to fold a stream event into the message.
var errAssemble = errors.New("could not assemble the streamed response")

// interrupted reports whether err ended a stream in a way worth retrying: the
// stream ended early, could not be assembled, or carried an overloaded or
// server error event after the response had begun (errors before it are
// retried by the SDK). A caller's cancel is never an interruption.
func interrupted(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, errIncomplete) || errors.Is(err, errAssemble) {
		return true
	}
	if status, ok := apiStatus(err); ok && status == 200 {
		t, _ := anthropicPayload(err)
		return t == "overloaded_error" || t == "api_error"
	}
	return false
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
