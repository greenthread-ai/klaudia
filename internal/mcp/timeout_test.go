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
// interrupted it — and a headless run forever.
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
	server, err := ConnectTransport(ctx, "s", clientT)
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
	if !res[0].IsError || !strings.Contains(res[0].Content, "timed out") {
		t.Errorf("result = %+v, want a timed-out error naming the setting", res[0])
	}
}

// Servers that never answer their handshake used to hold startup forever,
// one after another. Now each has a deadline and they start together.
func TestConnectDeadlineAndParallel(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("no sleep binary")
	}
	t.Setenv("KLAUDIA_MCP_CONNECT_TIMEOUT", "1")
	cfg := Config{MCPServers: map[string]ServerConfig{
		"mute1": {Command: "sleep", Args: []string{"30"}},
		"mute2": {Command: "sleep", Args: []string{"30"}},
		"mute3": {Command: "sleep", Args: []string{"30"}},
	}}
	start := time.Now()
	m, errs := Connect(context.Background(), cfg)
	took := time.Since(start)
	defer m.Close()
	if len(errs) != 3 {
		t.Fatalf("errs = %v, want three connect failures", errs)
	}
	if !strings.Contains(errs[0].Error(), "KLAUDIA_MCP_CONNECT_TIMEOUT") {
		t.Errorf("error %q should name the deadline setting", errs[0])
	}
	// Each mute server costs its 1s deadline plus the SDK's shutdown of a
	// child that ignores a closed stdin (about 5s before it is signalled):
	// about 6s each. One after another that is ~18s; together, ~6s.
	if took > 10*time.Second {
		t.Errorf("connecting three mute servers took %s; want them in parallel (~6s), not in turn (~18s)", took)
	}
	if len(m.Servers()) != 3 {
		t.Errorf("servers = %d, want placeholders kept for /mcp", len(m.Servers()))
	}
}
