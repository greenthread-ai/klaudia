package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var zeroRetries = 0

// noRetries keeps a test against an erroring server to a single request.
var noRetries = Tuning{MaxRetries: &zeroRetries}

func TestTuningDefaults(t *testing.T) {
	var zero Tuning
	if zero.maxRetries() != defaultMaxRetries || zero.streamIdleTimeout() != defaultStreamIdleTimeout {
		t.Errorf("zero Tuning = (%d, %v), want the defaults", zero.maxRetries(), zero.streamIdleTimeout())
	}
	nine, off := 9, time.Duration(0)
	set := Tuning{MaxRetries: &nine, StreamIdleTimeout: &off}
	if set.maxRetries() != 9 || set.streamIdleTimeout() != 0 {
		t.Errorf("set Tuning = (%d, %v), want (9, 0 = watchdog off)", set.maxRetries(), set.streamIdleTimeout())
	}
	neg := -1
	if (Tuning{MaxRetries: &neg}).maxRetries() != defaultMaxRetries {
		t.Error("a negative retry count should fall back to the default")
	}
}

// The tuning belongs to the provider, not the process: two clients in one
// process with different settings must each send what they were built with.
func TestNoPromptCacheIsPerClient(t *testing.T) {
	var (
		mu     sync.Mutex
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"sink"}}`, 500)
	}))
	defer srv.Close()

	uncached := noRetries
	uncached.NoPromptCache = true
	_, _ = New(Credential{APIKey: "sk-fake"}, srv.URL, uncached).StreamTurn(context.Background(), testParams(), StreamSink{})
	_, _ = New(Credential{APIKey: "sk-fake"}, srv.URL, noRetries).StreamTurn(context.Background(), testParams(), StreamSink{})

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("got %d requests, want 2", len(bodies))
	}
	if strings.Contains(bodies[0], "cache_control") {
		t.Error("NoPromptCache client sent cache_control")
	}
	if !strings.Contains(bodies[1], "cache_control") {
		t.Error("default client sent no cache_control")
	}
}
