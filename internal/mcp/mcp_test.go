package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread/klaudia/internal/tools"
)

type echoIn struct {
	Message string `json:"message" jsonschema:"the message to echo"`
}

// startTestServer spins up an in-process MCP server with one "echo" tool and
// returns a Manager connected to it over an in-memory transport.
func startTestServer(t *testing.T) (*Manager, func()) {
	t.Helper()
	ctx := context.Background()

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "testsrv", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "echo", Description: "Echo the message back"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, in echoIn) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "echo: " + in.Message}},
			}, nil, nil
		})

	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	server, err := ConnectTransport(ctx, "testsrv", clientT)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	m := &Manager{}
	m.Add(server)
	return m, func() { m.Close(); _ = ss.Wait() }
}

func TestManagerToolsWrapsAndCalls(t *testing.T) {
	m, cleanup := startTestServer(t)
	defer cleanup()

	wrapped := m.Tools(context.Background())
	var echo tools.Tool
	for _, w := range wrapped {
		if w.Name() == "mcp__testsrv__echo" {
			echo = w
		}
	}
	if echo == nil {
		t.Fatalf("echo tool not exposed; got %v", names(wrapped))
	}

	// The wrapped tool advertises a usable object schema.
	var sch map[string]any
	if err := json.Unmarshal(echo.InputSchema(), &sch); err != nil {
		t.Fatalf("schema invalid: %v", err)
	}

	raw, _ := json.Marshal(map[string]string{"message": "hi there"})
	res, err := echo.Execute(context.Background(), tools.Context{}, raw)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(res[0].Content, "echo: hi there") {
		t.Errorf("result = %q, want to contain 'echo: hi there'", res[0].Content)
	}
}

func TestLoadConfigMissingIsEmpty(t *testing.T) {
	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected empty config, got %v", cfg.MCPServers)
	}
}

func names(ts []tools.Tool) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name())
	}
	return out
}
