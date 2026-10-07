package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	oaText   = "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n"
	oaFinish = "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
)

func openAIScripted(t *testing.T, handle func(n int, w http.ResponseWriter)) (*OpenAIProvider, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle(int(attempts.Add(1))-1, w)
	}))
	t.Cleanup(srv.Close)
	return NewOpenAIProvider(srv.URL+"/v1", "k", nil, nil), &attempts
}

func oaParams() anthropic.BetaMessageNewParams {
	return anthropic.BetaMessageNewParams{Model: "m", MaxTokens: 16,
		Messages: []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))}}
}

// A reply whose connection closes before [DONE] or a finish_reason was cut
// off. Nothing shown yet: retried. Text shown: reported as interrupted.
func TestOpenAIStreamClosedEarly(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	p, attempts := openAIScripted(t, func(n int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 0 {
			return // closed with nothing sent
		}
		fmt.Fprint(w, oaText+oaFinish)
	})
	msg, err := p.StreamTurn(context.Background(), oaParams(), StreamSink{})
	if err != nil || attempts.Load() != 2 || msg.StopReason != "end_turn" {
		t.Fatalf("closed before output: err %v, attempts %d, stop %q; want a clean retry", err, attempts.Load(), msg.StopReason)
	}

	p, attempts = openAIScripted(t, func(n int, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, oaText) // text, then the connection closes
	})
	_, err = p.StreamTurn(context.Background(), oaParams(), StreamSink{OnText: func(string) {}})
	if !errors.Is(err, ErrStreamInterrupted) || !errors.Is(err, errStallMidStream) || attempts.Load() != 1 {
		t.Fatalf("closed after output: err %v, attempts %d; want one attempt reported as cut off", err, attempts.Load())
	}
}

// Retry-After was documented as honoured and never read.
func TestOpenAIRetryAfter(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "2")
	var first time.Time
	var gap time.Duration
	p, attempts := openAIScripted(t, func(n int, w http.ResponseWriter) {
		if n == 0 {
			first = time.Now()
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		gap = time.Since(first)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, oaText+oaFinish)
	})
	if _, err := p.StreamTurn(context.Background(), oaParams(), StreamSink{}); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 || gap < 900*time.Millisecond {
		t.Errorf("attempts %d, gap %v; want the retry after the 1s the server asked for", attempts.Load(), gap)
	}

	// A wait longer than maxRetryAfter is not sat through.
	p, attempts = openAIScripted(t, func(n int, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	start := time.Now()
	if _, err := p.StreamTurn(context.Background(), oaParams(), StreamSink{}); err == nil {
		t.Fatal("want the 429 surfaced")
	}
	if attempts.Load() != 1 || time.Since(start) > 2*time.Second {
		t.Errorf("attempts %d in %v; want one, returned at once", attempts.Load(), time.Since(start))
	}
}

func TestRetryAfterParse(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	for v, want := range map[string]time.Duration{
		"5":                             5 * time.Second,
		"Tue, 29 Sep 2026 12:00:30 GMT": 30 * time.Second,
		"Tue, 29 Sep 2026 11:00:00 GMT": 0,
	} {
		if got, ok := retryAfter(v, now); !ok || got != want {
			t.Errorf("retryAfter(%q) = %v,%v; want %v", v, got, ok, want)
		}
	}
	for _, v := range []string{"", "soon", "-3"} {
		if _, ok := retryAfter(v, now); ok {
			t.Errorf("retryAfter(%q) parsed; want not told", v)
		}
	}
}
