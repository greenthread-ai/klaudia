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
func TestMCPPermission(t *testing.T) {
	tests := []struct {
		name     string
		mode     permission.Mode
		trusting bool
		want     permission.Behavior
	}{
		{
			name:     "trusting sessions do not prompt",
			mode:     permission.ModeDefault,
			trusting: true,
			want:     permission.Allow,
		},
		{
			// The gate is what protects the machine, and it is running in both
			// cases; without it nothing has vouched for the call, so the prompt
			// is still the only thing standing there.
			name:     "untrusting sessions still ask",
			mode:     permission.ModeDefault,
			trusting: false,
			want:     permission.Ask,
		},
		{
			// Plan mode is read-only for every tool. Trust says who vouches for
			// a call, not whether the session is meant to be changing anything.
			name:     "plan mode refuses even when trusting",
			mode:     permission.ModePlan,
			trusting: true,
			want:     permission.Deny,
		},
		{
			name:     "plan mode refuses when not trusting",
			mode:     permission.ModePlan,
			trusting: false,
			want:     permission.Deny,
		},
		{
			// An unattended run has nobody to ask, so it used to refuse. With
			// trust enforcing, something has vouched and the run can proceed —
			// which is what makes MCP usable headlessly at all.
			name:     "dontAsk proceeds when trusting",
			mode:     permission.ModeDontAsk,
			trusting: true,
			want:     permission.Allow,
		},
		{
			name:     "dontAsk still refuses without trust",
			mode:     permission.ModeDontAsk,
			trusting: false,
			want:     permission.Deny,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pctx := permission.Context{
				Mode:     permission.StaticMode(tt.mode),
				Trusting: func() bool { return tt.trusting },
			}
			if got := mcpPermission(pctx).Behavior; got != tt.want {
				t.Errorf("mode=%s trusting=%v: got %s, want %s", tt.mode, tt.trusting, got, tt.want)
			}
		})
	}
}

// A Context built before trust existed — or by a test — has no Trusting probe.
// It must read as "nothing is vouching", which is the behaviour that was there
// before, rather than panicking or silently allowing.
func TestMCPPermissionWithoutTrustProbe(t *testing.T) {
	pctx := permission.Context{Mode: permission.StaticMode(permission.ModeDefault)}
	if got := mcpPermission(pctx).Behavior; got != permission.Ask {
		t.Errorf("a Context with no Trusting probe got %s, want %s", got, permission.Ask)
	}
	// And the fully zero value, which is what Options{} yields.
	if got := mcpPermission(permission.Context{}).Behavior; got != permission.Ask {
		t.Errorf("zero-value Context got %s, want %s", got, permission.Ask)
	}
}
