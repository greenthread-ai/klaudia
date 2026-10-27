package cli

import (
	"context"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/mcp"
)

// A failed reconnect changes nothing, so it must not rebuild the registries
// (rebuilding lists every live server's tools — a round trip each).
func TestFailedReconnectDoesNotRebuild(t *testing.T) {
	mgr, _ := mcp.Connect(context.Background(), mcp.Config{}, nil)
	rebuilt := false
	c := mcpController{mgr: mgr, ctx: context.Background(), rebuild: func() { rebuilt = true }}
	if err := c.Reconnect("absent"); err == nil {
		t.Fatal("reconnecting an unknown server should fail")
	}
	if rebuilt {
		t.Error("a failed reconnect rebuilt the registries")
	}
}
