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
