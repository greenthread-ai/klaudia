package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// safeWrap wraps an error for Unwrap testing WITHOUT invoking the inner error's
// Error() method (which the SDK's *anthropic.Error cannot satisfy when built by
// hand — it dereferences a nil Request/Response). FriendlyError uses errors.As,
// which unwraps without calling Error(), so its status branches stay safe.
type safeWrap struct{ inner error }

func (w safeWrap) Error() string { return "wrapped" }
func (w safeWrap) Unwrap() error { return w.inner }

func TestFriendlyErrorRateLimit(t *testing.T) {
	// Pass the API error directly: the 429 branch returns before .Error().
	msg := FriendlyError(&anthropic.Error{StatusCode: 429})
	if !strings.Contains(msg, "Rate limited (429)") {
		t.Errorf("got %q, want a rate-limit message", msg)
	}
	if strings.Contains(msg, "POST") {
		t.Errorf("friendly message should not include the raw POST dump: %q", msg)
	}
}

func TestFriendlyErrorUnwrapsThroughWrapper(t *testing.T) {
	err := safeWrap{inner: &anthropic.Error{StatusCode: 429}}
	if !strings.Contains(FriendlyError(err), "Rate limited (429)") {
		t.Error("FriendlyError should unwrap to the API error and classify it")
	}
}

func TestFriendlyErrorOpenAI(t *testing.T) {
	// The OpenAI-compatible provider's error must classify like Anthropic's.
	msg := FriendlyError(&OpenAIError{StatusCode: 429, Body: "rate limited"})
	if !strings.Contains(msg, "Rate limited (429)") {
		t.Errorf("OpenAI 429 not classified: %q", msg)
	}
	if FriendlyError(&OpenAIError{StatusCode: 401}) == "" ||
		!strings.Contains(FriendlyError(&OpenAIError{StatusCode: 401}), "Authentication failed") {
		t.Error("OpenAI 401 not classified as auth failure")
	}
}

func TestFriendlyError429HintIsProviderSpecific(t *testing.T) {
	// The Claude Code OAuth-token hint is Anthropic-specific and must not surface
	// for OpenAI-compatible providers (the original "even though we're using
	// ChatGPT we're told to check our Claude Code OAuth" bug).
	openAI := FriendlyError(&OpenAIError{StatusCode: 429})
	if strings.Contains(openAI, "OAuth") || strings.Contains(openAI, "Claude Code") {
		t.Errorf("OpenAI 429 leaked the Claude-Code OAuth hint: %q", openAI)
	}
	if !strings.Contains(openAI, "KLAUDIA_MAX_RETRIES") {
		t.Errorf("OpenAI 429 should still mention the retries env: %q", openAI)
	}
	anth := FriendlyError(&anthropic.Error{StatusCode: 429})
	if !strings.Contains(anth, "Claude Code OAuth") {
		t.Errorf("Anthropic 429 should mention the OAuth-token cause: %q", anth)
	}
}

func TestFriendlyErrorInsufficientQuotaDoesNotRecommendRetry(t *testing.T) {
	// Real OpenAI body for an exhausted-quota 429: the provider already gives a
	// useful message, and retries won't help — so we surface their wording and
	// drop the misleading "retries / KLAUDIA_MAX_RETRIES" advice.
	body := `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","param":null,"code":"insufficient_quota"}}`
	msg := FriendlyError(&OpenAIError{StatusCode: 429, Body: body})

	for _, want := range []string{"Insufficient quota", "exceeded your current quota", "Retries won't help"} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in: %q", want, msg)
		}
	}
	for _, banned := range []string{"KLAUDIA_MAX_RETRIES", "retries were exhausted", "Wait a moment"} {
		if strings.Contains(msg, banned) {
			t.Errorf("should not recommend retrying for insufficient_quota: %q (contains %q)", msg, banned)
		}
	}
}

func TestFriendlyError429PassesThroughOpenAIMessage(t *testing.T) {
	// For a non-quota 429 with a useful provider message, surface it verbatim
	// instead of the generic "API is busy" guess.
	body := `{"error":{"message":"You are sending requests too quickly.","type":"requests","code":"rate_limit_exceeded"}}`
	msg := FriendlyError(&OpenAIError{StatusCode: 429, Body: body})
	if !strings.Contains(msg, "You are sending requests too quickly") {
		t.Errorf("expected the upstream message to pass through: %q", msg)
	}
	if !strings.Contains(msg, "KLAUDIA_MAX_RETRIES") {
		t.Errorf("transient 429 should still mention the retry knob: %q", msg)
	}
}

func TestOpenAIErrorPayload(t *testing.T) {
	// Parses the OpenAI error envelope; returns nil for empty / non-JSON bodies.
	p := (&OpenAIError{Body: `{"error":{"message":"m","type":"t","code":"c"}}`}).Payload()
	if p == nil || p.Message != "m" || p.Type != "t" || p.Code != "c" {
		t.Errorf("payload = %+v", p)
	}
	if (&OpenAIError{Body: ""}).Payload() != nil {
		t.Error("empty body should parse to nil")
	}
	if (&OpenAIError{Body: "plain text"}).Payload() != nil {
		t.Error("non-JSON body should parse to nil")
	}
	if (&OpenAIError{Body: `{}`}).Payload() != nil {
		t.Error("envelope without an error object should parse to nil")
	}
}

func TestBackoffGrows(t *testing.T) {
	if backoff(1) >= backoff(2) {
		t.Error("backoff should grow with attempt")
	}
	if backoff(20) > 8*1e9 { // capped at 8s
		t.Error("backoff should be capped")
	}
}

func TestFriendlyErrorAuth(t *testing.T) {
	for _, code := range []int{401, 403} {
		msg := FriendlyError(&anthropic.Error{StatusCode: code})
		if !strings.Contains(msg, "Authentication failed") {
			t.Errorf("code %d: got %q", code, msg)
		}
	}
}

func TestFriendlyErrorPassthrough(t *testing.T) {
	if FriendlyError(errors.New("some non-api failure")) != "some non-api failure" {
		t.Errorf("non-API errors should pass through verbatim")
	}
	if FriendlyError(nil) != "" {
		t.Errorf("nil error should yield empty string")
	}
}

func TestFriendlyErrorNetwork(t *testing.T) {
	// A DNS failure (e.g. a placeholder/typo'd baseURL) wrapped like the real
	// stream error should hint at the endpoint, not dump a raw dial error.
	dns := &net.DNSError{Err: "no such host", Name: "api.example.com"}
	wrapped := fmt.Errorf("stream: %w", dns)
	msg := FriendlyError(wrapped)
	if !strings.Contains(msg, "Could not reach the model endpoint") || !strings.Contains(msg, "baseURL") {
		t.Errorf("DNS error not made friendly: %q", msg)
	}

	// A connection refused (*net.OpError) should also be softened.
	op := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	if !strings.Contains(FriendlyError(op), "Could not reach the model endpoint") {
		t.Errorf("OpError not made friendly: %q", FriendlyError(op))
	}

	// A timeout maps to the timeout hint.
	if !strings.Contains(FriendlyError(fmt.Errorf("x: %w", context.DeadlineExceeded)), "timed out") {
		t.Error("deadline-exceeded not classified as a timeout")
	}
}

func TestMaxRetriesEnvOverride(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "9")
	if maxRetries() != 9 {
		t.Errorf("maxRetries = %d, want 9", maxRetries())
	}
	t.Setenv("KLAUDIA_MAX_RETRIES", "")
	if maxRetries() != defaultMaxRetries {
		t.Errorf("maxRetries = %d, want default %d", maxRetries(), defaultMaxRetries)
	}
}
