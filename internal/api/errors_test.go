package api

import (
	"errors"
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
