package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"
)

// FallbackProvider wraps a Provider with a configured fallback model
// (fallbackModel in config, --fallback-model on the command line).
//
// Without one, an overloaded or unavailable model ends the turn: the SDK has
// already spent its retries, so the error is final. With one, two failures
// move to the fallback instead:
//
//   - Overloaded (529, 503, or an overloaded_error event mid-stream): the
//     request is retried once on the fallback model. The next request tries
//     the primary again — overload is transient, and the user chose the
//     primary.
//   - Model not found (a 404, or a provider's "model does not exist"): the
//     request is retried on the fallback, and every later request for that
//     model goes straight to the fallback for the rest of the session. A
//     model that does not exist will not exist on the next turn either.
//
// Nothing is retried once output has reached the sink: a second request would
// show the user the start of the reply twice. Nor is a user interrupt, or a
// request already on the fallback model.
//
// Because it sits at the Provider boundary, the fallback also covers
// compaction summaries and sub-agents, which issue their model calls through
// the same provider.
type FallbackProvider struct {
	inner    Provider
	fallback anthropic.Model

	mu sync.Mutex
	// missing holds the models the provider reported as not found; requests
	// for them are sent to the fallback without trying them first.
	missing map[anthropic.Model]bool
}

// WithFallback wraps inner with fallback when fallback is set, resolving
// aliases (haiku|sonnet|opus) the way --model does. An empty fallback returns
// inner unchanged, so an unconfigured session behaves exactly as before.
func WithFallback(inner Provider, fallback string) Provider {
	if strings.TrimSpace(fallback) == "" {
		return inner
	}
	return &FallbackProvider{
		inner:    inner,
		fallback: ResolveModel(fallback),
		missing:  map[anthropic.Model]bool{},
	}
}

// Fallback reports the resolved fallback model.
func (p *FallbackProvider) Fallback() anthropic.Model { return p.fallback }

// StreamTurn implements Provider.
func (p *FallbackProvider) StreamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink) (anthropic.BetaMessage, error) {
	primary := params.Model
	if primary == p.fallback {
		return p.inner.StreamTurn(ctx, params, sink)
	}
	if p.isMissing(primary) {
		return p.inner.StreamTurn(ctx, p.onFallback(params), sink)
	}

	// Track whether anything reached the caller, so a failure after output
	// is not retried. Callbacks the caller left nil stay nil: a nil
	// OnRawEvent is how the Anthropic client knows not to forward raw events.
	var delivered atomic.Bool
	tracked := sink
	if sink.OnText != nil {
		tracked.OnText = func(s string) {
			delivered.Store(true)
			sink.OnText(s)
		}
	}
	if sink.OnRawEvent != nil {
		tracked.OnRawEvent = func(ev anthropic.BetaRawMessageStreamEventUnion) {
			delivered.Store(true)
			sink.OnRawEvent(ev)
		}
	}

	msg, err := p.inner.StreamTurn(ctx, params, tracked)
	if err == nil || ctx.Err() != nil || delivered.Load() {
		return msg, err
	}
	switch {
	case IsModelNotFound(err):
		p.markMissing(primary)
		sink.notice(fmt.Sprintf("Model %s was not found; using fallback model %s for the rest of this session.", primary, p.fallback))
	case IsOverloaded(err):
		sink.notice(fmt.Sprintf("Model %s is overloaded; retrying this request on fallback model %s.", primary, p.fallback))
	default:
		return msg, err
	}
	msg, ferr := p.inner.StreamTurn(ctx, p.onFallback(params), sink)
	if ferr != nil {
		return msg, fmt.Errorf("fallback model %s: %w", p.fallback, ferr)
	}
	return msg, nil
}

// onFallback returns params re-targeted at the fallback model. The output cap
// was sized for the primary; when the fallback is a model with a known,
// smaller cap, it is lowered to fit, since the API rejects a max_tokens above
// the model's limit. An unknown fallback keeps the caller's cap — the table's
// floor for unknown models would silently shrink an explicit setting.
func (p *FallbackProvider) onFallback(params anthropic.BetaMessageNewParams) anthropic.BetaMessageNewParams {
	params.Model = p.fallback
	if limit, ok := modelMaxOutputTokens[string(p.fallback)]; ok && params.MaxTokens > int64(limit) {
		params.MaxTokens = int64(limit)
	}
	return params
}

func (p *FallbackProvider) isMissing(m anthropic.Model) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.missing[m]
}

func (p *FallbackProvider) markMissing(m anthropic.Model) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.missing[m] = true
}

// IsOverloaded reports whether err is the provider saying it cannot serve the
// model right now: a 529 (Anthropic's overloaded), a 503, or an
// overloaded_error — which also arrives as an SSE error event on a stream
// that had already started with a 200.
func IsOverloaded(err error) bool {
	if err == nil {
		return false
	}
	if status, ok := apiStatus(err); ok {
		if status == 529 || status == 503 {
			return true
		}
		errType, _ := anthropicPayload(err)
		return errType == "overloaded_error"
	}
	// The SDK falls back to a plain error carrying the raw event when it
	// cannot decode a mid-stream error event.
	return strings.Contains(err.Error(), "overloaded_error")
}

// IsModelNotFound reports whether err is the provider rejecting the request's
// model as unknown. Anthropic answers a 404 not_found_error with the message
// "model: <id>"; OpenAI-compatible hosts send a 404 (sometimes a 400) with a
// model_not_found code or "The model `<id>` does not exist". A 404 that does
// not mention a model — a wrong baseURL path, say — is not matched: switching
// models would not fix it.
func IsModelNotFound(err error) bool {
	if err == nil {
		return false
	}
	status, ok := apiStatus(err)
	if !ok || (status != 404 && status != 400) {
		return false
	}
	if oai := openAIPayload(err); oai != nil && codeIs(oai.Code, "model_not_found") {
		return true
	}
	detail := strings.ToLower(providerDetail(err))
	if detail == "" {
		var oai *OpenAIError
		if errors.As(err, &oai) {
			detail = strings.ToLower(oai.Body) // plain-text error bodies
		}
	}
	if !strings.Contains(detail, "model") {
		return false
	}
	if errType, _ := anthropicPayload(err); errType == "not_found_error" {
		return true
	}
	for _, s := range []string{"model_not_found", "not found", "not_found", "does not exist", "invalid model", "unknown model"} {
		if strings.Contains(detail, s) {
			return true
		}
	}
	return false
}
