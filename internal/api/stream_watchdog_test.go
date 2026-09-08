package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// stallingServer answers /v1/messages with a 200 text/event-stream and then
// sends nothing, modeling a half-open SSE connection (proxy/NAT dropped the
// flow, or the server stalled after the response head). It unblocks when the
// client cancels the request, and counts how many attempts arrived.
func stallingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // hang until the client gives up
	}))
	t.Cleanup(srv.Close)
	return srv, &attempts
}

func testParams() anthropic.BetaMessageNewParams {
	return anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(DefaultModel),
		MaxTokens: 16,
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
	}
}

// A stalled stream that has delivered nothing is retried transparently up to
// maxStreamStallRetries, then surfaces as a DeadlineExceeded error rather than
// hanging forever.
func TestStreamRetryingStallRetriesThenFails(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	srv, attempts := stallingServer(t)
	c := New(Credential{APIKey: "sk-fake"}, srv.URL)

	done := make(chan error, 1)
	go func() {
		_, err := c.streamRetrying(context.Background(), testParams(), StreamSink{}, 80*time.Millisecond)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want wrapped context.DeadlineExceeded", err)
		}
		if got := attempts.Load(); got != maxStreamStallRetries+1 {
			t.Fatalf("attempts = %d, want %d (initial + %d retries)", got, maxStreamStallRetries+1, maxStreamStallRetries)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("streamRetrying did not return — watchdog failed to break the stall")
	}
}

// A user interrupt cancels the caller's ctx. That must surface as
// context.Canceled (so the loop treats it as an interrupt, not a stall) and
// must NOT trigger a stall retry.
func TestStreamRetryingUserCancelNotRetried(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	srv, attempts := stallingServer(t)
	c := New(Credential{APIKey: "sk-fake"}, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// Long idle so the watchdog cannot fire first — the cancel below wins.
		_, err := c.streamRetrying(ctx, testParams(), StreamSink{}, time.Hour)
		done <- err
	}()
	time.AfterFunc(50*time.Millisecond, cancel)

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if got := attempts.Load(); got != 1 {
			t.Fatalf("attempts = %d, want 1 (a user interrupt must not retry)", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("streamRetrying did not return after ctx cancel")
	}
}

// oaStallingServer answers /chat/completions with a 200 text/event-stream that
// then sends nothing — the OpenAI-shim equivalent of a half-open SSE stall that
// would otherwise park bufio.Scanner.Scan() forever.
func oaStallingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv, &attempts
}

// The OpenAI shim's watchdog breaks a mid-stream stall and reports it as a stall
// (not a user cancel) so StreamTurn can apply the same retry policy.
func TestOpenAIStreamAttemptStallTripsWatchdog(t *testing.T) {
	srv, attempts := oaStallingServer(t)
	p := NewOpenAIProvider(srv.URL, "k", nil)

	done := make(chan struct{}, 1)
	go func() {
		_, _, stalled, err := p.streamAttempt(context.Background(), []byte(`{}`), "m", StreamSink{}, 80*time.Millisecond)
		if !stalled {
			t.Errorf("stalled = false, want true (idle watchdog should trip)")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("err = %v, want context.DeadlineExceeded", err)
		}
		if got := attempts.Load(); got != 1 {
			t.Errorf("attempts = %d, want 1 (streamAttempt issues one request)", got)
		}
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("streamAttempt did not return — OpenAI watchdog failed")
	}
}

// A user interrupt (ctx cancel) on the OpenAI path is NOT a stall.
func TestOpenAIStreamAttemptUserCancelNotStall(t *testing.T) {
	srv, _ := oaStallingServer(t)
	p := NewOpenAIProvider(srv.URL, "k", nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 1)
	go func() {
		_, _, stalled, err := p.streamAttempt(ctx, []byte(`{}`), "m", StreamSink{}, time.Hour)
		if stalled {
			t.Errorf("stalled = true, want false (a user interrupt is not a stall)")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		done <- struct{}{}
	}()
	time.AfterFunc(50*time.Millisecond, cancel)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("streamAttempt did not return after ctx cancel")
	}
}

func TestStreamIdleTimeoutOverride(t *testing.T) {
	t.Setenv("KLAUDIA_STREAM_IDLE_TIMEOUT", "7")
	if got := streamIdleTimeout(); got != 7*time.Second {
		t.Fatalf("streamIdleTimeout() = %v, want 7s", got)
	}
	t.Setenv("KLAUDIA_STREAM_IDLE_TIMEOUT", "0")
	if got := streamIdleTimeout(); got != 0 {
		t.Fatalf("streamIdleTimeout() = %v, want 0 (disabled)", got)
	}
	t.Setenv("KLAUDIA_STREAM_IDLE_TIMEOUT", "")
	if got := streamIdleTimeout(); got != defaultStreamIdleTimeout {
		t.Fatalf("streamIdleTimeout() = %v, want default %v", got, defaultStreamIdleTimeout)
	}
}

// h2 keepalive pings are what stop a connection that died while the session was
// idle from looking usable. The values matter (a ping must resolve well inside
// the stall watchdog), so assert them rather than trusting the wiring.
func TestConfigureH2EnablesKeepalivePings(t *testing.T) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	h2, err := configureH2(tr)
	if err != nil {
		t.Fatalf("configureH2: %v", err)
	}
	if h2.ReadIdleTimeout != h2ReadIdleTimeout {
		t.Errorf("ReadIdleTimeout = %s, want %s", h2.ReadIdleTimeout, h2ReadIdleTimeout)
	}
	if h2.PingTimeout != h2PingTimeout {
		t.Errorf("PingTimeout = %s, want %s", h2.PingTimeout, h2PingTimeout)
	}
	if h2.PingTimeout >= defaultStreamIdleTimeout {
		t.Error("a dead connection must be detected before the stall watchdog fires")
	}
}

// countingTransport records CloseIdleConnections calls; http.Client delegates
// to the transport when it implements the method.
type countingTransport struct {
	http.RoundTripper
	closed atomic.Int32
}

func (c *countingTransport) CloseIdleConnections() { c.closed.Add(1) }

func TestStallRetryDropsIdleConnections(t *testing.T) {
	srv, attempts := stallingServer(t)
	tr := &countingTransport{RoundTripper: http.DefaultTransport}
	httpc := &http.Client{Transport: tr}
	c := &Client{
		sdk:   anthropic.NewClient(option.WithBaseURL(srv.URL), option.WithAPIKey("test"), option.WithHTTPClient(httpc), option.WithMaxRetries(0)),
		httpc: httpc,
	}

	_, err := c.streamRetrying(context.Background(), testParams(), StreamSink{}, 50*time.Millisecond)
	if !errors.Is(err, ErrStreamStalled) {
		t.Fatalf("err = %v, want a stall", err)
	}
	// Retrying onto the same dead pooled connection is the bug; each retry
	// must dial afresh.
	if got, want := tr.closed.Load(), int32(maxStreamStallRetries); got != want {
		t.Errorf("CloseIdleConnections called %d times, want %d (attempts=%d)", got, want, attempts.Load())
	}
}

// pingingServer streams SSE keepalives, then completes a minimal message. The
// SDK discards ping events entirely, so this is a stream that is alive on the
// wire and silent as far as stream.Next() is concerned.
func pingingServer(t *testing.T, pings int, gap time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, _ := w.(http.Flusher)
		flush := func(s string) {
			_, _ = io.WriteString(w, s)
			if f != nil {
				f.Flush()
			}
		}
		flush("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n")
		for i := 0; i < pings; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(gap):
			}
			flush("event: ping\ndata: {\"type\":\"ping\"}\n\n")
		}
		flush("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		flush("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The reported failure: a stall mid-turn on a healthy connection. Pings kept
// the socket busy but the SDK swallows them, so an event-counting watchdog saw
// silence and killed a working stream.
func TestPingsKeepTheStreamAlive(t *testing.T) {
	idle := 150 * time.Millisecond
	srv := pingingServer(t, 6, idle/2) // 6 gaps of idle/2 ≈ 3 idle windows
	c := &Client{sdk: anthropic.NewClient(
		option.WithBaseURL(srv.URL), option.WithAPIKey("test"),
		option.WithHTTPClient(newHTTPClient()), option.WithMaxRetries(0),
	)}

	_, err := c.streamRetrying(context.Background(), testParams(), StreamSink{}, idle)
	if errors.Is(err, ErrStreamStalled) {
		t.Fatalf("a stream sending keepalives must not be treated as stalled: %v", err)
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The watchdog must still fire when the bytes genuinely stop.
func TestSilentStreamStillStalls(t *testing.T) {
	srv, _ := stallingServer(t)
	c := &Client{sdk: anthropic.NewClient(
		option.WithBaseURL(srv.URL), option.WithAPIKey("test"),
		option.WithHTTPClient(newHTTPClient()), option.WithMaxRetries(0),
	)}

	_, err := c.streamRetrying(context.Background(), testParams(), StreamSink{}, 100*time.Millisecond)
	if !errors.Is(err, ErrStreamStalled) {
		t.Fatalf("err = %v, want a stall", err)
	}
}
