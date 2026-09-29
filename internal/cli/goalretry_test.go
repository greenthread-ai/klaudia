package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/api"
)

func fastBackoff(t *testing.T) {
	t.Helper()
	old := goalRetryBackoff
	goalRetryBackoff = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { goalRetryBackoff = old })
}

// One overloaded response used to end an hours-long goal run.
func TestRetryTransientRecovers(t *testing.T) {
	fastBackoff(t)
	calls := 0
	var out bytes.Buffer
	res, err := retryTransient(context.Background(), &out, "iteration 3", func() (agent.Result, error) {
		calls++
		if calls < 3 {
			return agent.Result{}, fmt.Errorf("stream: %w", api.ErrStreamStalled)
		}
		return agent.Result{Text: "done"}, nil
	})
	if err != nil || res.Text != "done" || calls != 3 {
		t.Fatalf("res %+v err %v calls %d; want success on the third try", res, err, calls)
	}
	if !strings.Contains(out.String(), "iteration 3 failed") {
		t.Errorf("retries not reported: %q", out.String())
	}
}

func TestRetryTransientGivesUpAndSkipsPermanentErrors(t *testing.T) {
	fastBackoff(t)
	calls := 0
	_, err := retryTransient(context.Background(), &bytes.Buffer{}, "iteration 1", func() (agent.Result, error) {
		calls++
		return agent.Result{}, api.ErrStreamStalled
	})
	if err == nil || calls != 1+len(goalRetryBackoff) {
		t.Errorf("calls = %d, err %v; want %d tries then the error", calls, err, 1+len(goalRetryBackoff))
	}

	calls = 0
	_, err = retryTransient(context.Background(), &bytes.Buffer{}, "iteration 1", func() (agent.Result, error) {
		calls++
		return agent.Result{}, errors.New("invalid x-api-key")
	})
	if err == nil || calls != 1 {
		t.Errorf("a non-transient error was retried (%d calls)", calls)
	}
}

func TestIsTransient(t *testing.T) {
	status := func(code int) error { return &anthropic.Error{StatusCode: code} }
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{status(429), true},
		{status(529), true},
		{status(503), true},
		{status(400), false},
		{status(401), false},
		{fmt.Errorf("x: %w", api.ErrStreamStalled), true},
		{context.Canceled, false},
		{errors.New("prompt is too long: 250000 tokens > 200000 maximum"), false},
		{nil, false},
	} {
		if got := api.IsTransient(tc.err); got != tc.want {
			t.Errorf("IsTransient(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
