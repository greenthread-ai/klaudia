package mcp

import (
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// MCP calls used to prompt every time, in every mode, no matter what else the
// session had established. The prompt was also close to unanswerable: "may
// mcp__gsol__godot_game_time run?" is not a question a user has the information
// to decide, and approving it bought one qualified name — a renamed server or
// the next tool on the same server started again from nothing.
//
// The host gate runs ahead of this and is not something the session can turn
// off, so there is no longer an "untrusting" case to ask about: a configured
// MCP server is trusted roughly as much as the shell, which is the same bet as
// running it at all.
func TestMCPPermission(t *testing.T) {
	tests := []struct {
		name string
		mode permission.Mode
		want permission.Behavior
	}{
		{
			name: "autonomous does not prompt",
			mode: permission.ModeAutonomous,
			want: permission.Allow,
		},
		{
			// Plan mode is read-only for every tool. Who vouches for a call is
			// a different question from whether the session is meant to be
			// changing anything.
			name: "plan mode refuses",
			mode: permission.ModePlan,
			want: permission.Deny,
		},
		{
			name: "bypass does not prompt",
			mode: permission.ModeBypassPermissions,
			want: permission.Allow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pctx := permission.Context{Mode: permission.StaticMode(tt.mode)}
			if got := mcpPermission(pctx, false).Behavior; got != tt.want {
				t.Errorf("mode=%s: got %s, want %s", tt.mode, got, tt.want)
			}
		})
	}
}

// A zero-value Context is what Options{} yields and what a test builds without
// a session. It reads as autonomous, so an MCP call runs: the thing that would
// have stopped it is the host gate, which is upstream of this and unaffected.
func TestMCPPermissionZeroValueContext(t *testing.T) {
	if got := mcpPermission(permission.Context{}, false).Behavior; got != permission.Allow {
		t.Errorf("zero-value Context got %s, want %s", got, permission.Allow)
	}
}

// Read-only sub-agents run in plan mode and are handed read-only MCP tools.
// Refusing those in plan mode made them uncallable; a tool marked read-only
// is allowed there, one that is not stays refused.
func TestPlanModeAllowsReadOnlyMCPTools(t *testing.T) {
	pctx := permission.Context{Mode: permission.StaticMode(permission.ModePlan)}
	if got := mcpPermission(pctx, true).Behavior; got != permission.Allow {
		t.Errorf("read-only tool in plan mode: got %s, want allow", got)
	}
	if got := mcpPermission(pctx, false).Behavior; got != permission.Deny {
		t.Errorf("unmarked tool in plan mode: got %s, want deny", got)
	}
}
