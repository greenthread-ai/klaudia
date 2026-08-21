package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// captureTool records the tools.Context it was executed with.
type captureTool struct{ got tools.Context }

func (c *captureTool) Name() string                                { return "Capture" }
func (c *captureTool) Description(context.Context) (string, error) { return "", nil }
func (c *captureTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (c *captureTool) ValidateInput(json.RawMessage) error { return nil }
func (c *captureTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (c *captureTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (c *captureTool) Execute(_ context.Context, tctx tools.Context, _ json.RawMessage) ([]tools.Result, error) {
	c.got = tctx
	return []tools.Result{{Content: "ok"}}, nil
}

// The bug: tools.Context was built without WorkingDir, so Bash ran in Klaudia's
// own process cwd rather than the project — and any path-based policy built on
// top of that would be meaningless.
func TestToolsReceiveTheWorkingDir(t *testing.T) {
	capture := &captureTool{}
	l := New(nil, tools.NewRegistry(capture))
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "Capture", Input: map[string]any{}}

	l.dispatch(context.Background(), tu, Options{WorkingDir: "/project/root"},
		nil, func(...string) {}, map[string]int{}, map[string]errStreak{})

	if capture.got.WorkingDir != "/project/root" {
		t.Errorf("tool ran with WorkingDir = %q, want the project root", capture.got.WorkingDir)
	}
}

// Sub-agents share the parent's project root; without it their tools would run
// in the process cwd while the parent's ran in the project.
func TestSpawnerCarriesTheWorkingDir(t *testing.T) {
	s := NewSpawner(nil, tools.NewRegistry(), "m", permission.Context{}, nil, 1).
		WithWorkingDir("/project/root")
	if s.workingDir != "/project/root" {
		t.Errorf("spawner workingDir = %q", s.workingDir)
	}
}
