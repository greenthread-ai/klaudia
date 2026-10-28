package mcp

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// A tool call that never returns used to hold the turn until the user
// interrupted it, and a headless run for good. (PR #38.)
func TestMCPToolCallTimesOut(t *testing.T) {
	t.Setenv("KLAUDIA_MCP_TOOL_TIMEOUT", "1")
	ctx := context.Background()
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "s", Version: "0"}, nil)
	srv.AddTool(&mcpsdk.Tool{Name: "stuck", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
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
	defer func() { m.Close(); _ = ss.Wait() }()

	start := time.Now()
	res, err := m.Tools(ctx)[0].Execute(ctx, tools.Context{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("call took %s, want it cut off near the 1s timeout", took)
	}
	if !res[0].IsError || !strings.Contains(res[0].Content, "no answer within 1s") || !strings.Contains(res[0].Content, "KLAUDIA_MCP_TOOL_TIMEOUT") {
		t.Errorf("result = %+v, want a timeout naming the setting", res[0])
	}
}

// The per-server setting beats the environment.
func TestToolTimeoutPrecedence(t *testing.T) {
	t.Setenv("KLAUDIA_MCP_TOOL_TIMEOUT", "")
	if got := toolTimeout(ServerConfig{}); got != 10*time.Minute {
		t.Errorf("default = %s, want 10m", got)
	}
	t.Setenv("KLAUDIA_MCP_TOOL_TIMEOUT", "90")
	if got := toolTimeout(ServerConfig{}); got != 90*time.Second {
		t.Errorf("env = %s, want 90s", got)
	}
	if got := toolTimeout(ServerConfig{Timeout: 5}); got != 5*time.Second {
		t.Errorf("server timeout = %s, want 5s", got)
	}
}

// Servers connect together: three that never answer cost one deadline, not
// three in a row.
func TestConnectInParallel(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep binary")
	}
	old := connectTimeout
	connectTimeout = time.Second
	t.Cleanup(func() { connectTimeout = old })
	cfg := Config{MCPServers: map[string]ServerConfig{
		"mute1": {Command: "sleep", Args: []string{"30"}},
		"mute2": {Command: "sleep", Args: []string{"30"}},
		"mute3": {Command: "sleep", Args: []string{"30"}},
	}}
	start := time.Now()
	m, errs := Connect(context.Background(), cfg, nil)
	took := time.Since(start)
	defer m.Close()
	if len(errs) != 3 {
		t.Fatalf("errs = %v, want three connect failures", errs)
	}
	if took > 2500*time.Millisecond {
		t.Errorf("connecting three mute servers took %s; want them in parallel (~1s), not in turn (~3s)", took)
	}
	if s := m.Servers(); len(s) != 3 || s[0].Name != "mute1" || s[2].Name != "mute3" {
		t.Errorf("servers = %d, want placeholders kept for /mcp in name order", len(s))
	}
}
