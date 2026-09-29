package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// A server that pages its tool list lost every tool past the first page.
func TestToolsReadsEveryPage(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "s", Version: "0"}, &mcpsdk.ServerOptions{PageSize: 2})
	for i := 0; i < 5; i++ {
		srv.AddTool(&mcpsdk.Tool{Name: fmt.Sprintf("t%d", i), InputSchema: json.RawMessage(`{"type":"object"}`)},
			func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
				return &mcpsdk.CallToolResult{}, nil
			})
	}
	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatal(err)
	}
	server, err := ConnectTransport(ctx, "s", clientT)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	m.Add(server)
	defer func() { m.Close(); _ = ss.Wait() }()

	if got := len(m.Tools(ctx)); got != 5 {
		t.Errorf("tools = %d, want all 5 across 3 pages of 2", got)
	}
	if err := server.ListError(); err != nil {
		t.Errorf("ListError = %v, want nil", err)
	}
}

// A failed list is recorded, not mistaken for a server with no tools.
func TestToolsRecordsListFailure(t *testing.T) {
	listRetryDelay = 0
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "s", Version: "0"}, nil)
	srv.AddTool(&mcpsdk.Tool{Name: "t", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{}, nil
		})
	srv.AddReceivingMiddleware(func(next mcpsdk.MethodHandler) mcpsdk.MethodHandler {
		return func(ctx context.Context, method string, req mcpsdk.Request) (mcpsdk.Result, error) {
			if method == "tools/list" {
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
	server, err := ConnectTransport(ctx, "s", clientT)
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	m.Add(server)
	defer func() { m.Close(); _ = ss.Wait() }()

	if got := len(m.Tools(ctx)); got != 0 {
		t.Errorf("tools = %d, want 0", got)
	}
	if err := server.ListError(); err == nil {
		t.Error("ListError = nil; a failed tools/list must be recorded")
	}
}
