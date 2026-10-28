package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// listFailingServer serves one tool and fails its first `failures`
// tools/list requests.
func listFailingServer(t *testing.T, failures int32) (*Manager, *Server) {
	t.Helper()
	old := listRetryDelay
	listRetryDelay = 0
	t.Cleanup(func() { listRetryDelay = old })
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "s", Version: "0"}, nil)
	srv.AddTool(&mcpsdk.Tool{Name: "t", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{}, nil
		})
	var lists atomic.Int32
	srv.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/list" && lists.Add(1) <= failures {
				return nil, fmt.Errorf("backend down")
			}
			return next(ctx, method, req)
		}
	})
	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ConnectTransport(ctx, "s", clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	m.Add(server)
	t.Cleanup(func() { m.Close(); _ = ss.Wait() })
	return m, server
}

// A failed list is recorded, not mistaken for a server with no tools. (PR #39.)
func TestToolsRecordsListFailure(t *testing.T) {
	m, server := listFailingServer(t, 100)
	if got := len(m.Tools(context.Background())); got != 0 {
		t.Errorf("tools = %d, want 0", got)
	}
	if err := server.ListError(); err == nil {
		t.Error("ListError = nil; a failed tools/list must be recorded")
	}
}

// One failure is retried, and success clears the error.
func TestToolsRetriesAFailedListOnce(t *testing.T) {
	m, server := listFailingServer(t, 1)
	if got := len(m.Tools(context.Background())); got != 1 {
		t.Errorf("tools = %d, want the one tool after a retry", got)
	}
	if err := server.ListError(); err != nil {
		t.Errorf("ListError = %v after a successful retry", err)
	}
}
