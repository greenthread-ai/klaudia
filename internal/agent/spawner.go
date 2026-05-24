package agent

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/subagent"
	"github.com/greenthread/klaudia/internal/tools"
)

// Spawner runs sub-agents. It implements tools.Spawner so the Agent tool can
// launch a child loop with a filtered toolset and the type's system prompt.
type Spawner struct {
	client     *api.Client
	base       *tools.Registry
	model      anthropic.Model
	permission permission.Context
	maxTurns   int
}

// NewSpawner builds a Spawner. base is the registry sub-agents draw tools from
// (typically the local tools without the Agent tool itself, to bound recursion).
func NewSpawner(client *api.Client, base *tools.Registry, model anthropic.Model, perm permission.Context, maxTurns int) *Spawner {
	return &Spawner{client: client, base: base, model: model, permission: perm, maxTurns: maxTurns}
}

// Spawn runs a sub-agent of the named type to completion and returns its final
// text. It satisfies tools.Spawner.
func (s *Spawner) Spawn(ctx context.Context, subagentType, prompt string) (string, error) {
	t, ok := subagent.Lookup(subagentType)
	if !ok {
		return "", fmt.Errorf("unknown subagent_type %q", subagentType)
	}
	loop := New(s.client, t.Filter(s.base))
	res, err := loop.Run(ctx, Options{
		Prompt:        prompt,
		Model:         s.model,
		System:        t.SystemPrompt,
		MaxTurns:      s.maxTurns,
		Permission:    s.permission,
		Interactive:   false,
		ContextWindow: 0,
	}, nil)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}
