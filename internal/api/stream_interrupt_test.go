package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	sseStart = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"x\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"
	sseText = "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"
	sseEnd  = "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	sseOverloaded = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	sseInvalid    = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"bad\"}}\n\n"
)

// scriptedSSE answers attempt n with bodies[n] (the last one repeats).
func scriptedSSE(t *testing.T, bodies ...string) (*Client, *atomic.Int32) {
	t.Helper()
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(attempts.Add(1)) - 1
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, bodies[min(n, len(bodies)-1)])
	}))
	t.Cleanup(srv.Close)
	return New(Credential{APIKey: "sk-fake"}, srv.URL), &attempts
}

func TestStreamInterruptions(t *testing.T) {
	complete := sseStart + sseText + sseEnd
	for _, tc := range []struct {
		name         string
		bodies       []string
		wantAttempts int32
		wantErr      bool
		wantMid      bool // reported as cut off after partial output
	}{
		// A clean close with nothing shown is retried, and the retry completes.
		{"closed before output", []string{sseStart, complete}, 2, false, false},
		// An overloaded event before any text is retried the same way.
		{"overloaded before output", []string{sseStart + sseOverloaded, complete}, 2, false, false},
		// After text has been shown, a retry would repeat it: report instead.
		{"closed after output", []string{sseStart + sseText}, 1, true, true},
		{"overloaded after output", []string{sseStart + sseText + sseOverloaded}, 1, true, true},
		// Retries are bounded.
		{"always closed early", []string{sseStart}, maxStreamStallRetries + 1, true, false},
		// A request error is not a transient interruption.
		{"invalid request event", []string{sseStart + sseInvalid, complete}, 1, true, false},
		{"complete", []string{complete}, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, attempts := scriptedSSE(t, tc.bodies...)
			var shown strings.Builder
			msg, err := c.streamRetrying(context.Background(), testParams(), StreamSink{OnText: func(s string) { shown.WriteString(s) }}, 0)
			if got := attempts.Load(); got != tc.wantAttempts {
				t.Errorf("attempts = %d, want %d", got, tc.wantAttempts)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if !tc.wantErr && msg.StopReason != "end_turn" {
				t.Errorf("stop reason %q, want end_turn", msg.StopReason)
			}
			if tc.wantMid {
				if !errors.Is(err, ErrStreamInterrupted) || !errors.Is(err, errStallMidStream) {
					t.Errorf("err = %v, want an interrupted-after-output error", err)
				}
				if f := FriendlyError(err); !strings.Contains(f, "cut off") {
					t.Errorf("FriendlyError = %q, want it to say the reply was cut off", f)
				}
			}
		})
	}
}
