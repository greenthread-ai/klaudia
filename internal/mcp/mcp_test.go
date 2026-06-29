package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
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

func TestDisconnectMakesToolFailGracefully(t *testing.T) {
	m, cleanup := startTestServer(t)
	defer cleanup()
	ctx := context.Background()

	tl := m.Tools(ctx)
	if len(tl) == 0 {
		t.Fatal("expected at least one tool")
	}
	if !m.Servers()[0].Connected() {
		t.Fatal("server should start connected")
	}

	if err := m.Disconnect("testsrv"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if m.Servers()[0].Connected() {
		t.Error("server should be disconnected")
	}
	// Calling the (still-registered) tool must not panic; it returns an error.
	res, err := tl[0].Execute(ctx, tools.Context{}, json.RawMessage(`{"message":"hi"}`))
	if err != nil {
		t.Fatalf("Execute returned a hard error: %v", err)
	}
	if len(res) == 0 || !res[0].IsError {
		t.Errorf("expected a graceful error result, got %+v", res)
	}
	// A disconnected server contributes no tools to the live list.
	if len(m.Tools(ctx)) != 0 {
		t.Error("disconnected server should expose no tools")
	}
}

func TestLoadConfigParsesHTTPServer(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{
		"mcpServers": {
			"remote": {"type": "http", "url": "https://mcp.example.com/v1"},
			"local":  {"command": "my-server", "args": ["--stdio"]}
		}
	}`), 0o644)

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r := cfg.MCPServers["remote"]; r.URL != "https://mcp.example.com/v1" || r.Type != "http" {
		t.Errorf("remote = %+v", r)
	}
	if l := cfg.MCPServers["local"]; l.Command != "my-server" || len(l.Args) != 1 {
		t.Errorf("local = %+v", l)
	}
}

func TestConnectServerRejectsEmptyConfig(t *testing.T) {
	// No command and no url → a clear error, without attempting any connection.
	if _, err := connectServer(context.Background(), "bad", ServerConfig{}); err == nil {
		t.Error("expected error for a config with neither command nor url")
	}
}

func TestReconnectUnknownServer(t *testing.T) {
	m := &Manager{cfg: Config{MCPServers: map[string]ServerConfig{}}}
	if err := m.Reconnect("nope"); err == nil {
		t.Error("expected error reconnecting unknown server")
	}
	if err := m.Disconnect("nope"); err == nil {
		t.Error("expected error disconnecting unknown server")
	}
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

func TestLoadConfigKlaudiaOverride(t *testing.T) {
	dir := t.TempDir()
	// Base ./.mcp.json defines two servers.
	os.WriteFile(filepath.Join(dir, ".mcp.json"),
		[]byte(`{"mcpServers":{"a":{"command":"a-base"},"b":{"command":"b"}}}`), 0o644)
	// .klaudia/.mcp.json overrides "a" and adds "c".
	os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755)
	os.WriteFile(filepath.Join(dir, ".klaudia", ".mcp.json"),
		[]byte(`{"mcpServers":{"a":{"command":"a-override"},"c":{"command":"c"}}}`), 0o644)

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MCPServers["a"].Command != "a-override" {
		t.Errorf("a = %q, want a-override (.klaudia wins)", cfg.MCPServers["a"].Command)
	}
	if cfg.MCPServers["b"].Command != "b" || cfg.MCPServers["c"].Command != "c" {
		t.Errorf("expected base b and override c: %+v", cfg.MCPServers)
	}
}

func names(ts []tools.Tool) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name())
	}
	return out
}
