package api

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// anthropicErr builds an *anthropic.Error the way the SDK does: the envelope
// from the body, the status from the response.
func anthropicErr(t *testing.T, status int, body string) *anthropic.Error {
	t.Helper()
	var e anthropic.Error
	if err := e.UnmarshalJSON([]byte(body)); err != nil {
		t.Fatalf("unmarshal anthropic.Error: %v", err)
	}
	e.StatusCode = status
	return &e
}

const (
	overloadedBody = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`
	notFoundBody   = `{"type":"error","error":{"type":"not_found_error","message":"model: claude-nonexistent"}}`
)

// modelProvider answers per model: an error from errs, or a message naming
// the model that served it. Optionally streams text before failing.
type modelProvider struct {
	mu      sync.Mutex
	errs    map[anthropic.Model]error
	textFor map[anthropic.Model]string // streamed to the sink before errs applies
	calls   []anthropic.BetaMessageNewParams
}

func (p *modelProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, sink StreamSink) (anthropic.BetaMessage, error) {
	p.mu.Lock()
	p.calls = append(p.calls, params)
	err := p.errs[params.Model]
	text := p.textFor[params.Model]
	p.mu.Unlock()
	if text != "" {
		sink.text(text)
	}
	if err != nil {
		return anthropic.BetaMessage{}, err
	}
	return anthropic.BetaMessage{Model: params.Model, StopReason: "end_turn"}, nil
}

func (p *modelProvider) models() []anthropic.Model {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]anthropic.Model, len(p.calls))
	for i, c := range p.calls {
		out[i] = c.Model
	}
	return out
}

func sameModels(a, b []anthropic.Model) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type noticeLog struct{ lines []string }

func (n *noticeLog) sink() StreamSink {
	return StreamSink{OnText: func(string) {}, OnNotice: func(s string) { n.lines = append(n.lines, s) }}
}

func req(model string) anthropic.BetaMessageNewParams {
	return anthropic.BetaMessageNewParams{Model: anthropic.Model(model), MaxTokens: 1000}
}

func TestWithFallbackUnsetLeavesProviderAlone(t *testing.T) {
	inner := &modelProvider{}
	if got := WithFallback(inner, "  "); got != Provider(inner) {
		t.Fatalf("an unset fallback must not wrap the provider, got %T", got)
	}
}

func TestFallbackRetriesOverloadedRequestOnce(t *testing.T) {
	inner := &modelProvider{errs: map[anthropic.Model]error{
		"primary": anthropicErr(t, 529, overloadedBody),
	}}
	p := WithFallback(inner, "backup")
	var n noticeLog

	msg, err := p.StreamTurn(context.Background(), req("primary"), n.sink())
	if err != nil {
		t.Fatalf("overload should be served by the fallback, got %v", err)
	}
	if msg.Model != "backup" {
		t.Errorf("served by %q, want backup", msg.Model)
	}
	if len(n.lines) != 1 || !strings.Contains(n.lines[0], "overloaded") || !strings.Contains(n.lines[0], "backup") {
		t.Errorf("the user must be told about the switch, got %q", n.lines)
	}

	// Overload is transient: the next request tries the primary again.
	_, _ = p.StreamTurn(context.Background(), req("primary"), n.sink())
	want := []anthropic.Model{"primary", "backup", "primary", "backup"}
	if got := inner.models(); !sameModels(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestFallbackSwitchesForTheSessionWhenModelNotFound(t *testing.T) {
	inner := &modelProvider{errs: map[anthropic.Model]error{
		"primary": anthropicErr(t, 404, notFoundBody),
	}}
	p := WithFallback(inner, "backup")
	var n noticeLog

	for i := 0; i < 3; i++ {
		if _, err := p.StreamTurn(context.Background(), req("primary"), n.sink()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	// One failed attempt on the primary, then the fallback for everything.
	want := []anthropic.Model{"primary", "backup", "backup", "backup"}
	if got := inner.models(); !sameModels(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if len(n.lines) != 1 || !strings.Contains(n.lines[0], "rest of this session") {
		t.Errorf("want one notice announcing the session-wide switch, got %q", n.lines)
	}
}

func TestFallbackDoesNotRetryAfterOutput(t *testing.T) {
	inner := &modelProvider{
		errs:    map[anthropic.Model]error{"primary": anthropicErr(t, 200, overloadedBody)},
		textFor: map[anthropic.Model]string{"primary": "partial answer"},
	}
	p := WithFallback(inner, "backup")

	_, err := p.StreamTurn(context.Background(), req("primary"), StreamSink{OnText: func(string) {}})
	if err == nil {
		t.Fatal("a failure after output must surface, not be retried")
	}
	if got := inner.models(); !sameModels(got, []anthropic.Model{"primary"}) {
		t.Errorf("calls = %v; a retry would repeat the partial answer", got)
	}
}

func TestFallbackLeavesOtherErrorsAlone(t *testing.T) {
	cases := map[string]error{
		"bad request": anthropicErr(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"messages: roles must alternate"}}`),
		"auth":        anthropicErr(t, 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`),
		"network":     errors.New("dial tcp: connection refused"),
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			inner := &modelProvider{errs: map[anthropic.Model]error{"primary": e}}
			_, err := WithFallback(inner, "backup").StreamTurn(context.Background(), req("primary"), StreamSink{})
			if !errors.Is(err, e) {
				t.Errorf("err = %v, want the original", err)
			}
			if len(inner.calls) != 1 {
				t.Errorf("calls = %v, want no fallback attempt", inner.models())
			}
		})
	}
}

func TestFallbackDoesNotRetryAnInterrupt(t *testing.T) {
	inner := &modelProvider{errs: map[anthropic.Model]error{"primary": anthropicErr(t, 529, overloadedBody)}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WithFallback(inner, "backup").StreamTurn(ctx, req("primary"), StreamSink{}); err == nil {
		t.Fatal("want the error")
	}
	if len(inner.calls) != 1 {
		t.Errorf("calls = %v, want no fallback attempt after an interrupt", inner.models())
	}
}

func TestFallbackOnTheFallbackModelIsNotRetried(t *testing.T) {
	inner := &modelProvider{errs: map[anthropic.Model]error{"claude-haiku-4-5": anthropicErr(t, 529, overloadedBody)}}
	// The alias resolves the way --model does.
	_, err := WithFallback(inner, "haiku").StreamTurn(context.Background(), req("claude-haiku-4-5"), StreamSink{})
	if err == nil || len(inner.calls) != 1 {
		t.Errorf("err = %v, calls = %v; a request already on the fallback has nowhere to go", err, inner.models())
	}
}

func TestFallbackFailureNamesTheFallback(t *testing.T) {
	over := anthropicErr(t, 529, overloadedBody)
	inner := &modelProvider{errs: map[anthropic.Model]error{"primary": over, "backup": over}}
	_, err := WithFallback(inner, "backup").StreamTurn(context.Background(), req("primary"), StreamSink{})
	if err == nil || !strings.Contains(err.Error(), "fallback model backup") {
		t.Fatalf("err = %v, want it to name the fallback that also failed", err)
	}
	// Still an overloaded error underneath, so FriendlyError explains it.
	if !strings.Contains(FriendlyError(err), "overloaded (529)") {
		t.Errorf("FriendlyError = %q", FriendlyError(err))
	}
}

func TestFallbackLowersOutputCapToAKnownFallbackLimit(t *testing.T) {
	inner := &modelProvider{errs: map[anthropic.Model]error{"claude-opus-5": anthropicErr(t, 529, overloadedBody)}}
	params := req("claude-opus-5")
	params.MaxTokens = 128000
	if _, err := WithFallback(inner, "haiku").StreamTurn(context.Background(), params, StreamSink{}); err != nil {
		t.Fatal(err)
	}
	if got := inner.calls[1].MaxTokens; got != 64000 {
		t.Errorf("fallback max_tokens = %d, want haiku's 64000", got)
	}

	// An unknown fallback keeps the caller's cap rather than the table floor.
	inner = &modelProvider{errs: map[anthropic.Model]error{"primary": anthropicErr(t, 529, overloadedBody)}}
	params = req("primary")
	params.MaxTokens = 32000
	if _, err := WithFallback(inner, "openai/some-model").StreamTurn(context.Background(), params, StreamSink{}); err != nil {
		t.Fatal(err)
	}
	if got := inner.calls[1].MaxTokens; got != 32000 {
		t.Errorf("fallback max_tokens = %d, want the caller's 32000", got)
	}
}

func TestIsOverloaded(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"anthropic 529", anthropicErr(t, 529, overloadedBody), true},
		{"mid-stream overloaded_error event", anthropicErr(t, 200, overloadedBody), true},
		{"undecoded mid-stream event", errors.New(`received error while streaming: {"type":"overloaded_error"}`), true},
		{"openai 503", &OpenAIError{StatusCode: 503, Body: "Service Unavailable"}, true},
		{"rate limit", anthropicErr(t, 429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`), false},
		{"500", &OpenAIError{StatusCode: 500}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := IsOverloaded(c.err); got != c.want {
			t.Errorf("%s: IsOverloaded = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsModelNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"anthropic 404", anthropicErr(t, 404, notFoundBody), true},
		{"openai model_not_found code", &OpenAIError{StatusCode: 404, Body: `{"error":{"message":"The model 'gpt-9' does not exist or you do not have access to it.","type":"invalid_request_error","code":"model_not_found"}}`}, true},
		{"vllm 404", &OpenAIError{StatusCode: 404, Body: `{"object":"error","message":"The model ` + "`llama-9`" + ` does not exist.","type":"NotFoundError","code":404}`}, true},
		{"plain-text 404", &OpenAIError{StatusCode: 404, Body: "model not found"}, true},
		{"proxy 400", &OpenAIError{StatusCode: 400, Body: `{"error":{"message":"Invalid model name passed in model=foo","type":"invalid_request_error"}}`}, true},
		{"wrong path 404", &OpenAIError{StatusCode: 404, Body: "404 page not found"}, false},
		{"unrelated 400", anthropicErr(t, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: must be at least 1"}}`), false},
		{"overloaded", anthropicErr(t, 529, overloadedBody), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := IsModelNotFound(c.err); got != c.want {
			t.Errorf("%s: IsModelNotFound = %v, want %v", c.name, got, c.want)
		}
	}
}
