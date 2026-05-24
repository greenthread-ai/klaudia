package tools

import (
	"testing"

	"github.com/greenthread/klaudia/internal/permission"
)

// Web/network tools must never be auto-allowed: they reach the network and can
// drive a persistent-profile browser, so they ask by default and are denied in
// the read-only modes.
func TestWebToolsArePermissionGated(t *testing.T) {
	ws, _ := NewWebSearch(nil)
	wf, _ := NewWebFetch(nil)
	bn, _ := NewBrowserNavigate(nil)
	bs, _ := NewBrowserSnapshot(nil)
	web := []Tool{ws, wf, bn, bs}

	cases := []struct {
		mode permission.Mode
		want permission.Behavior
	}{
		{permission.ModeDefault, permission.Ask},
		{permission.ModeAcceptEdits, permission.Ask}, // acceptEdits is for file edits, not network
		{permission.ModePlan, permission.Deny},
		{permission.ModeDontAsk, permission.Deny},
	}
	for _, tool := range web {
		for _, c := range cases {
			got := tool.CheckPermissions(permission.Context{Mode: c.mode}, permission.PermissionRequest{})
			if got.Behavior != c.want {
				t.Errorf("%s in %s: behavior = %q, want %q", tool.Name(), c.mode, got.Behavior, c.want)
			}
		}
	}
}
