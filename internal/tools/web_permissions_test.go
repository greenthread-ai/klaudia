package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// Web/network tools must never be auto-allowed: they reach the network and can
// drive a persistent-profile browser, so they ask by default and are denied in
// the read-only modes.
func TestWebToolsArePermissionGated(t *testing.T) {
	ws, _ := NewBrowserSearch(nil)
	wf, _ := NewBrowserFetch(nil)
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
			got := tool.CheckPermissions(permission.Context{Mode: permission.StaticMode(c.mode)}, permission.PermissionRequest{})
			if got.Behavior != c.want {
				t.Errorf("%s in %s: behavior = %q, want %q", tool.Name(), c.mode, got.Behavior, c.want)
			}
		}
	}
}

func TestWebToolsReturnErrorWhenBrowserEngineMissing(t *testing.T) {
	ws, err := NewBrowserSearch(nil)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := NewBrowserFetch(nil)
	if err != nil {
		t.Fatal(err)
	}
	bn, err := NewBrowserNavigate(nil)
	if err != nil {
		t.Fatal(err)
	}
	bs, err := NewBrowserSnapshot(nil)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		tool Tool
		raw  json.RawMessage
	}{
		{"BrowserSearch", ws, json.RawMessage(`{"query":"example"}`)},
		{"BrowserFetch", wf, json.RawMessage(`{"url":"https://example.com"}`)},
		{"BrowserNavigate", bn, json.RawMessage(`{"url":"https://example.com"}`)},
		{"BrowserSnapshot", bs, json.RawMessage(`{}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := tc.tool.Execute(context.Background(), Context{}, tc.raw)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if len(res) != 1 || !res[0].IsError {
				t.Fatalf("Execute() = %+v, want one error result", res)
			}
		})
	}
}
