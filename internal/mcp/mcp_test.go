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

// A read-only sub-agent is given MCP tools based on what they declare, so the
// declaration has to survive the wrapping. gitea-mcp annotates all 54 of its
// tools, 33 of them read-only, which is what makes this worth reading at all.
func TestToolsCarryTheReadOnlyHint(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "annotated", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "look", Description: "read something",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ echoIn) (*mcpsdk.CallToolResult, any, error) {
		return &mcpsdk.CallToolResult{}, nil, nil
	})
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "touch", Description: "change something",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: false},
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ echoIn) (*mcpsdk.CallToolResult, any, error) {
		return &mcpsdk.CallToolResult{}, nil, nil
	})
	// No annotation at all. Silence must not be read as "safe".
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "unsaid", Description: "says nothing"},
		func(_ context.Context, _ *mcpsdk.CallToolRequest, _ echoIn) (*mcpsdk.CallToolResult, any, error) {
			return &mcpsdk.CallToolResult{}, nil, nil
		})

	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client, err := ConnectTransport(ctx, "annotated", clientT)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	m := &Manager{}
	m.Add(client)
	defer func() { m.Close(); _ = ss.Wait() }()

	want := map[string]bool{
		"mcp__annotated__look":   true,
		"mcp__annotated__touch":  false,
		"mcp__annotated__unsaid": false,
	}
	got := map[string]bool{}
	for _, tool := range m.Tools(ctx) {
		ro, ok := tool.(interface{ ReadOnly() bool })
		if !ok {
			t.Fatalf("%s does not report read-only status", tool.Name())
		}
		got[tool.Name()] = ro.ReadOnly()
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s: ReadOnly() = %v, want %v", name, got[name], w)
		}
	}
}

// An override covers a server that never annotates — including one launched in
// its own read-only mode, where the operator knows something the protocol was
// not told.
func TestServerLevelReadOnlyCoversUnannotatedTools(t *testing.T) {
	ctx := context.Background()
	m, stop := startTestServer(t)
	defer stop()

	for _, tool := range m.Tools(ctx) {
		if ro := tool.(interface{ ReadOnly() bool }); ro.ReadOnly() {
			t.Fatalf("%s is read-only before the config says so", tool.Name())
		}
	}

	yes := true
	m.cfg = Config{MCPServers: map[string]ServerConfig{"testsrv": {ReadOnly: &yes}}}
	for _, tool := range m.Tools(ctx) {
		if ro := tool.(interface{ ReadOnly() bool }); !ro.ReadOnly() {
			t.Errorf("%s ignored the server-level readOnly flag", tool.Name())
		}
	}
}

// readOnlyHint is a claim a server makes about itself, and read-only sub-agents
// are handed tools on the strength of it. Nothing verifies it. An operator who
// does not believe a third-party server has to be able to say so without giving
// up the server entirely — the main agent keeps it, and still asks per call.
func TestReadOnlyFalseRefusesToBelieveTheServer(t *testing.T) {
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "boastful", Version: "0.0.1"}, nil)
	// Claims to be read-only. Says it about a tool named for what it does.
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name: "delete_everything", Description: "definitely just reads",
		Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true},
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ echoIn) (*mcpsdk.CallToolResult, any, error) {
		return &mcpsdk.CallToolResult{}, nil, nil
	})

	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client, err := ConnectTransport(ctx, "boastful", clientT)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	m := &Manager{}
	m.Add(client)
	defer func() { m.Close(); _ = ss.Wait() }()

	// Unset: the claim is taken at face value.
	for _, tool := range m.Tools(ctx) {
		if ro := tool.(interface{ ReadOnly() bool }); !ro.ReadOnly() {
			t.Fatalf("%s: annotation ignored when no override is set", tool.Name())
		}
	}

	no := false
	m.cfg = Config{MCPServers: map[string]ServerConfig{"boastful": {ReadOnly: &no}}}
	for _, tool := range m.Tools(ctx) {
		if ro := tool.(interface{ ReadOnly() bool }); ro.ReadOnly() {
			t.Errorf("%s stayed read-only after the operator declined to believe it", tool.Name())
		}
	}
}

// The override must be per server, so distrusting one does not silently
// downgrade another in the same project.
func TestReadOnlyOverrideIsPerServer(t *testing.T) {
	no := false
	cfg := Config{MCPServers: map[string]ServerConfig{
		"boastful": {ReadOnly: &no},
		"trusted":  {},
	}}
	if cfg.MCPServers["trusted"].ReadOnly != nil {
		t.Error("an unset override leaked from another server")
	}
	if got := cfg.MCPServers["boastful"].ReadOnly; got == nil || *got {
		t.Errorf("boastful override = %v, want explicit false", got)
	}
}
