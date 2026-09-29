package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/subagent"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Spawner runs sub-agents. It implements tools.Spawner so the Agent tool can
// launch a child loop with a filtered toolset and the type's system prompt.
type Spawner struct {
	provider   api.Provider
	base       *tools.Registry
	model      anthropic.Model
	permission permission.Context
	approver   Approver
	maxTurns   int

	// mu guards deferredTools, which an MCP config reload replaces while the
	// agent loop is reading it to spawn a sub-agent. The map is swapped
	// wholesale and never mutated in place, so a reader that has taken a
	// reference may safely use it after releasing the lock.
	mu            sync.RWMutex
	deferredTools map[string]bool

	workingDir string
	hostGate   *HostGate

	// background tracks sub-agents launched with SpawnBackground; worktrees
	// isolates the writers among them. Both are set lazily so a Spawner built
	// without them (older callers, tests) still runs synchronous sub-agents.
	background *BackgroundRegistry
	worktrees  WorktreeProvider
}

// Background returns the registry of background sub-agents this Spawner has
// launched, creating it on first use. The frontend lists it (/agents) and the
// loop collects finished results from it (PendingReport).
func (s *Spawner) Background() *BackgroundRegistry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.background == nil {
		s.background = NewBackgroundRegistry()
	}
	return s.background
}

// WithWorktrees sets the provider that isolates background writers. Passing nil
// disables isolation (writers then share the parent tree — acceptable for a
// single writer, unsafe for concurrent ones). The default wiring supplies a
// git-backed provider; tests inject a fake.
func (s *Spawner) WithWorktrees(w WorktreeProvider) *Spawner {
	s.worktrees = w
	return s
}

// SetDeferred replaces the deferred-tool set. A config reload can add or drop
// MCP tools mid-session, and a sub-agent spawned afterwards should inherit the
// set as it is now, not as it was at startup.
func (s *Spawner) SetDeferred(deferred map[string]bool) {
	next := make(map[string]bool, len(deferred))
	for name, ok := range deferred {
		next[name] = ok
	}
	s.mu.Lock()
	s.deferredTools = next
	s.mu.Unlock()
}

// deferred returns the current deferred-tool set.
func (s *Spawner) deferred() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.deferredTools
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
	// Relay the child's activity upward. Without an emitter the child ran
	// completely dark: the frontend saw one Agent tool call and nothing until it
	// returned, so a twenty-minute research run and a hang looked identical.
	return s.runChild(ctx, t, prompt, s.workingDir, progressEmitter(progress))
}

// progressEmitter adapts a per-line progress callback into a child Emitter.
// Passing nil returns nil, restoring the previous silent behaviour.
func progressEmitter(progress func(string)) Emitter {
	if progress == nil {
		return nil
	}
	return func(ev Event) {
		if line := subagentProgressLine(ev); line != "" {
			progress(line)
		}
	}
}

// runChild runs one sub-agent loop to completion in workingDir and returns its
// final text, applying the shared turn/context bounds. It is the single body
// both the synchronous Spawn and the background goroutine drive.
func (s *Spawner) runChild(ctx context.Context, t subagent.Type, prompt, workingDir string, emit Emitter) (string, error) {
	childTools := t.Filter(s.base)

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
		WorkingDir:    workingDir,
		Approver:      s.approver,
		ContextWindow: ctxWindow,
		DeferredTools: filterDeferred(s.deferred(), childTools),
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

// SpawnBackground launches a sub-agent that runs independently of the parent
// turn and returns its handle immediately. The result is delivered later, via
// the registry (PendingReport → Options.CollectBackground) and shown live by
// the /agents view. label is the model's task description, for the status view.
//
// A writer type (subagent.Type.MayWrite) runs in an isolated git worktree when
// a provider is configured, so concurrent writers cannot corrupt each other's
// tree; a read-only type shares the parent tree. The child runs on a context
// detached from the parent turn — it must outlive the turn that started it — so
// its cancellation is tracked in the registry (Cancel) rather than tied to ctx.
func (s *Spawner) SpawnBackground(subagentType, prompt, label string, progress func(string)) (string, error) {
	t, ok := subagent.Lookup(subagentType)
	if !ok {
		return "", fmt.Errorf("unknown subagent_type %q", subagentType)
	}

	reg := s.Background()
	isolate := t.MayWrite() && s.worktrees != nil && s.workingDir != ""

	ctx, cancel := context.WithCancel(context.Background())
	id := reg.register(subagentType, label, isolate, cancel)

	// Background progress cannot go to the launching tool call — that returned
	// the moment we handed back the id — so it updates the registry entry, which
	// is what the /agents view reads for a live activity line.
	emit := progressEmitter(func(line string) { reg.setActivity(id, line) })

	go func() {
		defer cancel()
		workingDir := s.workingDir
		var cleanup func() error
		if isolate {
			dir, cl, err := s.worktrees.Create(ctx, s.workingDir, id)
			if err != nil {
				// Isolation is a safety property, not a nicety: rather than run a
				// writer in the shared tree and risk corrupting it, fail the agent
				// and say why.
				reg.finish(id, "", fmt.Errorf("could not isolate a worktree: %w", err))
				return
			}
			workingDir, cleanup = dir, cl
		}
		if cleanup != nil {
			defer func() { _ = cleanup() }()
		}
		result, err := s.runChild(ctx, t, prompt, workingDir, emit)
		reg.finish(id, result, err)
	}()

	return id, nil
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
