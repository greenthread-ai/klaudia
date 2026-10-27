package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/hooks"
)

// No runner is no hooks: a nil interface, not one holding a nil pointer.
func TestHooksForNil(t *testing.T) {
	if hooksFor(nil) != nil {
		t.Fatal("hooksFor(nil) must be a nil agent.Hooks")
	}
}

// The adapter carries an event through to the runner and its result back.
func TestHooksAdapterRoundTrip(t *testing.T) {
	r := hooks.New(t.TempDir(), "sess", []config.Hook{{Event: "UserPromptSubmit", Command: `echo "on branch main"`}}, nil)
	r.Store = hooks.Store{Path: filepath.Join(t.TempDir(), "hooks.json")}
	h := hooksFor(r)
	if !h.Has(agent.HookUserPromptSubmit) || h.Has(agent.HookPreToolUse) {
		t.Fatal("Has should report exactly the configured event")
	}
	res := h.Run(context.Background(), agent.HookInput{Event: agent.HookUserPromptSubmit, CWD: t.TempDir(), Prompt: "hi"}, nil)
	if !strings.Contains(res.Context, "on branch main") {
		t.Errorf("result = %+v, want the hook's output as context", res)
	}
}
