package agent

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/subagent"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Spawner runs sub-agents. It implements tools.Spawner so the Agent tool can
// launch a child loop with a filtered toolset and the type's system prompt.
type Spawner struct {
	provider      api.Provider
	base          *tools.Registry
	model         anthropic.Model
	permission    permission.Context
	approver      Approver
	maxTurns      int
	deferredTools map[string]bool
	workingDir    string
	hostGate      *HostGate
}

// WithWorkingDir sets the project root sub-agents inherit. Without it a
// sub-agent's tools would run in the process cwd while the parent's run in the
// project — the kind of split that makes path-based policy meaningless.
func (s *Spawner) WithWorkingDir(dir string) *Spawner {
	s.workingDir = dir
	return s
}

// WithHostGate gives sub-agents the parent's trust gate.
//
// Sharing the gate — and therefore the ledger — is the point: a sub-agent must
// not be a way around the boundary, and an approval the user gave the parent
// should cover the child doing the work. Without this a sub-agent's Bash calls
// would be unclassified, which is the easiest hole to leave and the hardest to
// notice.
func (s *Spawner) WithHostGate(g *HostGate) *Spawner {
	s.hostGate = g
	return s
}

// NewSpawner builds a Spawner. base is the registry sub-agents draw tools from
// (typically the local tools without the Agent tool itself, to bound recursion).
// approver resolves permission asks for sub-agents (inherited from the parent
// frontend); nil falls back to DenyAll.
func NewSpawner(provider api.Provider, base *tools.Registry, model anthropic.Model, perm permission.Context, approver Approver, maxTurns int) *Spawner {
	return NewSpawnerWithDeferred(provider, base, model, perm, approver, maxTurns, nil)
}

// NewSpawnerWithDeferred builds a Spawner that also respects the parent's
// deferred tool map after applying the sub-agent type allowlist.
func NewSpawnerWithDeferred(provider api.Provider, base *tools.Registry, model anthropic.Model, perm permission.Context, approver Approver, maxTurns int, deferred map[string]bool) *Spawner {
	copyDeferred := make(map[string]bool, len(deferred))
	for name, ok := range deferred {
		copyDeferred[name] = ok
	}
	return &Spawner{provider: provider, base: base, model: model, permission: perm, approver: approver, maxTurns: maxTurns, deferredTools: copyDeferred}
}

// defaultSubagentMaxTurns bounds a sub-agent when the CLI sets no explicit
// --max-turns (its default is 0, "unlimited"). Unlimited is a defensible
// default for the main loop, where the user watches each step and can interrupt;
// for a child it means an invisible loop that can run until someone notices.
// Generous enough for real research, finite enough to end.
const defaultSubagentMaxTurns = 50

// Spawn runs a sub-agent of the named type to completion and returns its final
// text. It satisfies tools.Spawner.
//
// progress, when non-nil, receives a short line per child tool call. Passing
// nil (as headless callers do) restores the previous silent behaviour.
func (s *Spawner) Spawn(ctx context.Context, subagentType, prompt string, progress func(string)) (string, error) {
	t, ok := subagent.Lookup(subagentType)
	if !ok {
		return "", fmt.Errorf("unknown subagent_type %q", subagentType)
	}
	childTools := t.Filter(s.base)

	// Relay the child's activity upward. Without an emitter the child ran
	// completely dark: the frontend saw one Agent tool call and nothing until it
	// returned, so a twenty-minute research run and a hang looked identical.
	var emit Emitter
	if progress != nil {
		emit = func(ev Event) {
			if line := subagentProgressLine(ev); line != "" {
				progress(line)
			}
		}
	}

	maxTurns := s.maxTurns
	if maxTurns <= 0 {
		maxTurns = defaultSubagentMaxTurns
	}
	// Give the child the model's real window. Leaving this 0 fell back to the
	// 200k compaction default, so a sub-agent on a 1M model summarised its
	// history at a fifth of the room it actually had.
	ctxWindow, _ := api.ContextWindow(string(s.model), 0)

	loop := New(s.provider, childTools)
	res, err := loop.Run(ctx, Options{
		Prompt:        prompt,
		Model:         s.model,
		System:        t.SystemPrompt,
		MaxTurns:      maxTurns,
		Permission:    s.permission,
		Host:          s.hostGate,
		WorkingDir:    s.workingDir,
		Approver:      s.approver,
		ContextWindow: ctxWindow,
		DeferredTools: filterDeferred(s.deferredTools, childTools),
	}, emit)
	if err != nil {
		return "", err
	}
	// Say so rather than passing back a truncated answer as if it were complete.
	if res.StopReason == "max_turns" {
		note := fmt.Sprintf("[Sub-agent stopped at its %d-turn limit before finishing. "+
			"The result below may be incomplete.]", maxTurns)
		if res.Text == "" {
			return note, nil
		}
		return note + "\n\n" + res.Text, nil
	}
	return res.Text, nil
}

func filterDeferred(deferred map[string]bool, registry *tools.Registry) map[string]bool {
	if len(deferred) == 0 {
		return nil
	}
	out := map[string]bool{}
	for name, ok := range deferred {
		if !ok {
			continue
		}
		if _, exists := registry.Lookup(name); exists {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
