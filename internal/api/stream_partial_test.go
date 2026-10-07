package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// cutOffServer writes the given SSE frames, then hangs until the client gives
// up — a reply that was partly shown before the connection went quiet.
func cutOffServer(t *testing.T, frames ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		f, _ := w.(http.Flusher)
		for _, s := range frames {
			_, _ = io.WriteString(w, s)
		}
		if f != nil {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func textOf(m anthropic.BetaMessage) string {
	var s string
	for _, b := range m.Content {
		if b.Type == "text" {
			s += b.Text
		}
	}
	return s
}

// A mid-stream stall on the Anthropic path hands back the text already shown
// alongside the error, so the agent loop can keep it in the history.
func TestStallMidStreamReturnsPartialText(t *testing.T) {
	srv := cutOffServer(t,
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"The fix is to \"}}\n\n",
	)
	c := &Client{sdk: anthropic.NewClient(
		option.WithBaseURL(srv.URL), option.WithAPIKey("test"),
		option.WithHTTPClient(newHTTPClient()), option.WithMaxRetries(0),
	)}

	var shown string
	sink := StreamSink{OnText: func(d string) { shown += d }}
	acc, err := c.streamRetrying(context.Background(), testParams(), sink, 150*time.Millisecond)
	if !errors.Is(err, ErrStreamStalled) || !errors.Is(err, errStallMidStream) {
		t.Fatalf("err = %v, want a mid-stream stall", err)
	}
	if got := textOf(acc); got != shown || got != "The fix is to " {
		t.Fatalf("partial text = %q, shown = %q; want both %q", got, shown, "The fix is to ")
	}
}

// The OpenAI shim used to return an empty message when the body read failed,
// discarding text it had already streamed to the user.
func TestOpenAIStallReturnsPartialText(t *testing.T) {
	srv := cutOffServer(t,
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"The fix is to \"}}]}\n\n",
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"function\":{\"name\":\"Read\",\"arguments\":\"{\\\"file_\"}}]}}]}\n\n",
	)
	p := NewOpenAIProvider(srv.URL, "k", nil, nil)

	acc, _, stalled, err := p.streamAttempt(context.Background(), []byte(`{}`), "m", StreamSink{}, 150*time.Millisecond)
	if !stalled || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled = %v, err = %v; want a stall", stalled, err)
	}
	if got := textOf(acc); got != "The fix is to " {
		t.Fatalf("partial text = %q, want %q", got, "The fix is to ")
	}
	for _, b := range acc.Content {
		if b.Type == "tool_use" {
			t.Fatalf("an unfinished tool call must not come back: %+v", b)
		}
	}
}
