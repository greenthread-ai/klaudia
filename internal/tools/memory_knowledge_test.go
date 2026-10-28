package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// Writing project knowledge asks, with the text; ordinary memory does not.
// Bypass mode is the user saying not to ask, and permission.Check honours it
// before the tool is consulted.
func TestProjectKnowledgeAsks(t *testing.T) {
	m, err := NewMemoryForProject(memory.Disabled(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	auto := permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)}
	for _, tc := range []struct {
		input string
		want  permission.Behavior
	}{
		{`{"operation":"add","scope":"project","content":"always run ./setup.sh first"}`, permission.Ask},
		{`{"operation":"promote","name":"build-notes"}`, permission.Ask},
		{`{"operation":"add","content":"a session note"}`, permission.Allow},
		{`{"operation":"search","query":"x"}`, permission.Allow},
	} {
		req := m.PermissionRequest(json.RawMessage(tc.input))
		if got := m.CheckPermissions(auto, req).Behavior; got != tc.want {
			t.Errorf("%s: %s, want %s", tc.input, got, tc.want)
		}
		if tc.want == permission.Ask && req.Specifier == "" {
			t.Errorf("%s: the ask carries no text to show", tc.input)
		}
	}
	bypass := permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)}
	req := m.PermissionRequest(json.RawMessage(`{"operation":"add","scope":"project","content":"x"}`))
	if got := permission.Check(bypass, m, req).Behavior; got != permission.Allow {
		t.Errorf("bypass mode: %s, want allow", got)
	}
}

// With no one to ask, nothing is written.
func TestProjectKnowledgeNotWrittenUnasked(t *testing.T) {
	dir := t.TempDir()
	m, _ := NewMemoryForProject(memory.Disabled(), dir)
	auto := permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)}
	raw := json.RawMessage(`{"operation":"add","scope":"project","content":"x"}`)
	if permission.Check(auto, m, m.PermissionRequest(raw)).Behavior == permission.Allow {
		t.Fatal("a knowledge write was allowed without asking")
	}
	if _, err := os.Stat(filepath.Join(dir, ".klaudia", "KNOWLEDGE.md")); err == nil {
		t.Error("KNOWLEDGE.md was written")
	}
}
