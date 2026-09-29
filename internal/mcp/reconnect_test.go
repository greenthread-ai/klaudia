package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

func pingServer() *mcpsdk.Server {
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "s", Version: "0"}, nil)
	srv.AddTool(&mcpsdk.Tool{Name: "ping", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "pong"}}}, nil
		})
	return srv
}

// A server that speaks only the legacy SSE transport, configured with a url
// and no type, failed with an HTTP error.
func TestHTTPFallsBackToSSE(t *testing.T) {
	isolateConfigRoot(t)
	srv := pingServer()
	hs := httptest.NewServer(mcpsdk.NewSSEHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil))
	defer hs.Close()

	m, errs := Connect(context.Background(), Config{MCPServers: map[string]ServerConfig{"legacy": {URL: hs.URL}}})
	defer m.Close()
	if len(errs) > 0 {
		t.Fatalf("connect: %v", errs)
	}
	res, err := m.Tools(context.Background())[0].Execute(context.Background(), tools.Context{}, json.RawMessage(`{}`))
	if err != nil || res[0].IsError || !strings.Contains(res[0].Content, "pong") {
		t.Fatalf("call over SSE: %+v %v", res, err)
	}
}

// swappable serves through a handler that can be replaced, which is how a
// remote server restart looks to a client: the session it holds is unknown.
type swappable struct {
	mu sync.Mutex
	h  http.Handler
}

func (s *swappable) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	h := s.h
	s.mu.Unlock()
	h.ServeHTTP(w, r)
}

func (s *swappable) set(h http.Handler) { s.mu.Lock(); s.h = h; s.mu.Unlock() }

// After a remote server restarts, every call failed on the lost session until
// someone ran /mcp. The call now reconnects once and goes through.
func TestCallReconnectsAfterSessionLoss(t *testing.T) {
	isolateConfigRoot(t)
	newHandler := func() http.Handler {
		srv := pingServer()
		return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return srv }, nil)
	}
	sw := &swappable{h: newHandler()}
	hs := httptest.NewServer(sw)
	defer hs.Close()

	ctx := context.Background()
	m, errs := Connect(ctx, Config{MCPServers: map[string]ServerConfig{"remote": {Type: "http", URL: hs.URL}}})
	defer m.Close()
	if len(errs) > 0 {
		t.Fatalf("connect: %v", errs)
	}
	tool := m.Tools(ctx)[0]
	if res, _ := tool.Execute(ctx, tools.Context{}, json.RawMessage(`{}`)); res[0].IsError {
		t.Fatalf("first call: %s", res[0].Content)
	}

	sw.set(newHandler()) // the server "restarts" and forgets the session
	res, err := tool.Execute(ctx, tools.Context{}, json.RawMessage(`{}`))
	if err != nil || res[0].IsError || !strings.Contains(res[0].Content, "pong") {
		t.Fatalf("call after the restart = %+v, %v; want it to reconnect and succeed", res, err)
	}
}
