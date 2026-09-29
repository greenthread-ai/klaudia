package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// An MCP result went into the context whole, however large.
func TestMCPResultIsCapped(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	ctx := context.Background()
	big := strings.Repeat("log line\n", 20000) // ~180 KB
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "s", Version: "0"}, nil)
	srv.AddTool(&mcpsdk.Tool{Name: "logs", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: big}}}, nil
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

	res, err := m.Tools(ctx)[0].Execute(ctx, tools.Context{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res[0].Content); n > 40000 {
		t.Errorf("MCP result is %d bytes in context, want it capped", n)
	}
	if res[0].Full != big {
		t.Error("the full result should be kept for display")
	}
}
