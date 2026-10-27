package agent

import (
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/subagent"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// builtinTypes resolves sub-agent types the way the CLI does, so spawner tests
// exercise Klaudia's real Explore / Plan / general-purpose definitions.
func builtinTypes(name string) (SubagentType, bool) {
	t, ok := subagent.Lookup(name)
	if !ok {
		return SubagentType{}, false
	}
	return SubagentType{SystemPrompt: t.SystemPrompt, Mode: t.Mode, Tools: t.Filter}, true
}

func newTestSpawner(provider api.Provider, base *tools.Registry, model anthropic.Model, perm permission.Context, approver Approver, maxTurns int) *Spawner {
	return NewSpawner(provider, base, model, perm, approver, maxTurns).WithTypes(builtinTypes)
}

func newTestSpawnerWithDeferred(provider api.Provider, base *tools.Registry, model anthropic.Model, perm permission.Context, approver Approver, maxTurns int, deferred map[string]bool) *Spawner {
	return NewSpawnerWithDeferred(provider, base, model, perm, approver, maxTurns, deferred).WithTypes(builtinTypes)
}
