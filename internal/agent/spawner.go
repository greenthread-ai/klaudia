package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/hooks"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/subagent"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/worktree"
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
	hooks      *hooks.Runner
	worktrees  bool
	effort     api.Effort
	limits     api.ModelLimits
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

// WithHooks gives sub-agents the parent's hooks.
//
// Same argument as the host gate: a sub-agent must not be the way around a rule
// the user set. A PostToolUse formatter that does not run on a child's writes is
// a formatter with an exception nobody can see from the config file. Only the
// tool events fire for a child — see Options.SubAgent.
func (s *Spawner) WithHooks(h *hooks.Runner) *Spawner {
	s.hooks = h
	return s
}

// WithWorktrees decides whether a sub-agent that can write gets its own git
// checkout to write in, rather than sharing the user's.
//
// It applies to the writing types only. Explore and Plan hold Read, Glob and
// Grep, so there is nothing to isolate them from — and their whole output is
// paths, which an isolated checkout would make wrong.
func (s *Spawner) WithWorktrees(on bool) *Spawner {
	s.worktrees = on
	return s
}

// WithEffort sets the effort level sub-agents run at, independently of the
// parent session's.
//
// The default is inherit — the parent passes its own level — because a silent
// downgrade is the kind of change that degrades results months later with
// nothing to attribute it to. The knob exists so the trade can be measured:
// Explore fan-outs are the canonical low-effort workload (many short,
// mechanical searches), and running them below the main loop is a real saving
// for anyone who has checked it holds on their own work.
func (s *Spawner) WithEffort(e api.Effort) *Spawner {
	s.effort = e
	return s
}

// WithLimits gives sub-agents the parent's resolved sizing for the model they
// share, rather than letting the child re-derive it from the offline tables.
//
// The child runs the parent's model, so re-deriving can only produce a worse
// answer: a figure the provider reported, or a maxTokens override from the
// config, is lost and the child falls back to the table — or, for a model the
// table does not know, to the 8192 floor. A sub-agent writing a large file is
// exactly where that truncates. Unset (the zero value) keeps the old table
// lookup, so a Spawner built without this still behaves sanely.
func (s *Spawner) WithLimits(l api.ModelLimits) *Spawner {
	s.limits = l
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

	// Give a writing child its own checkout, so two of them running at once do
	// not edit one tree. Isolation is an improvement on sharing, never a
	// precondition for working: every way it can fail falls back to the shared
	// tree with a note, because refusing the task would be worse.
	dir := s.workingDir
	var tree *worktree.Tree
	if s.worktrees && writesFiles(childTools) && worktree.Supported(ctx, dir) {
		if wt, err := worktree.New(ctx, dir, subagentType); err != nil {
			reportf(progress, "  (sharing the working tree: %v)", err)
		} else {
			tree, dir = wt, wt.Dir
			reportf(progress, "  ↳ isolated checkout %s", wt.Dir)
		}
	}

	// Relay the child's activity upward. Without an emitter the child ran
	// completely dark: the frontend saw one Agent tool call and nothing until it
	// returned, so a twenty-minute research run and a hang looked identical.
	var emit Emitter
	if progress != nil {
		emit = func(ev Event) {
			if line := subagentProgressLine(ev); line != "" {
				// Paths in the child's own checkout are paths the user cannot
				// open; say where the file really is.
				progress(tree.Rewrite(line))
			}
		}
	}

	maxTurns := s.maxTurns
	if maxTurns <= 0 {
		maxTurns = defaultSubagentMaxTurns
	}
	// Give the child the model's real window. Leaving this 0 fell back to the
	// 200k compaction default, so a sub-agent on a 1M model summarised its
	// history at a fifth of the room it actually had. The parent's resolved
	// figures win when it supplied them (WithLimits); the table lookup is the
	// fallback for a Spawner wired without them.
	ctxWindow, maxTokens := s.limits.ContextWindow, s.limits.MaxOutputTokens
	if ctxWindow == 0 {
		ctxWindow, _ = api.ContextWindow(string(s.model), 0)
	}

	loop := New(s.provider, childTools)
	res, err := loop.Run(ctx, Options{
		Prompt:        prompt,
		Model:         s.model,
		System:        t.SystemPrompt,
		MaxTurns:      maxTurns,
		Permission:    s.permission,
		Host:          s.hostGate,
		WorkingDir:    dir,
		Approver:      s.approver,
		ContextWindow: ctxWindow,
		MaxTokens:     int64(maxTokens),
		Effort:        s.effort,
		DeferredTools: filterDeferred(s.deferred(), childTools),
		Hooks:         s.hooks,
		SubAgent:      true,
	}, emit)
	if err != nil {
		// Whatever the child wrote stays where it is. A failed or interrupted
		// run left the checkout in a state nobody has inspected, and applying
		// half a change to the user's tree is the one outcome isolation exists
		// to prevent — so the error says where to look instead.
		if tree != nil {
			return "", fmt.Errorf("%w (the sub-agent's changes are left in %s)", err, tree.Dir)
		}
		return "", err
	}
	text := res.Text
	if tree != nil {
		text = tree.Rewrite(text)
		// Cleanup must still happen when the turn's context has just been
		// cancelled, hence a context detached from it.
		done, cancel := context.WithTimeout(context.WithoutCancel(ctx), worktreeCleanupTimeout)
		defer cancel()
		text = s.collect(done, tree, text, progress)
	}
	// Say so rather than passing back a truncated answer as if it were complete.
	if res.StopReason == "max_turns" {
		note := fmt.Sprintf("[Sub-agent stopped at its %d-turn limit before finishing. "+
			"The result below may be incomplete.]", maxTurns)
		if text == "" {
			return note, nil
		}
		return note + "\n\n" + text, nil
	}
	return text, nil
}

// worktreeCleanupTimeout bounds adoption and removal. Both are a handful of git
// commands on a tree that is already on disk; a limit this generous only ever
// fires on a repository that has gone wrong, and the alternative is a tool call
// that never returns.
const worktreeCleanupTimeout = 2 * time.Minute

// collect applies the child's work to the user's tree and tells the model what
// landed.
//
// The model is told because it has to be: it asked a child to change files, and
// "b.txt was not applied" changes what it should do next. A silent conflict
// would have it carry on describing work that is not in the tree.
func (s *Spawner) collect(ctx context.Context, tree *worktree.Tree, text string, progress func(string)) string {
	rep, err := tree.Adopt(ctx)
	if err != nil {
		reportf(progress, "  (could not apply the sub-agent's changes: %v)", err)
		return text + fmt.Sprintf("\n\n[The sub-agent's file changes could not be applied to the "+
			"working tree (%v). They are in %s.]", err, tree.Dir)
	}
	if len(rep.Conflicted) > 0 {
		// The conflicting version exists only in the checkout, so removing it
		// would destroy the only copy of work the user may want.
		reportf(progress, "  ↳ %s", rep.Summary())
		return text + fmt.Sprintf("\n\n[Working tree: %s. The sub-agent's versions of those files "+
			"are in %s.]", rep.Summary(), tree.Dir)
	}
	if err := tree.Remove(ctx); err != nil {
		reportf(progress, "  (left the sub-agent's checkout at %s: %v)", tree.Dir, err)
	}
	if rep.Empty() {
		return text
	}
	reportf(progress, "  ↳ %s", rep.Summary())
	return text + fmt.Sprintf("\n\n[Working tree: %s.]", rep.Summary())
}

// writesFiles reports whether this toolset can change the working tree.
//
// Four names rather than a capability on the Tool interface: these are the
// local tools that touch files, and the list is short and stable. MCP tools are
// deliberately not counted even when they are not read-only — their writes land
// on a server, which a second checkout does not isolate, and counting them
// would hand a research agent a pointless copy of the repository.
func writesFiles(r *tools.Registry) bool {
	for _, name := range []string{"Write", "Edit", "NotebookEdit", "Bash"} {
		if _, ok := r.Lookup(name); ok {
			return true
		}
	}
	return false
}

// reportf sends one formatted progress line, if anyone is listening.
func reportf(progress func(string), format string, args ...any) {
	if progress == nil {
		return
	}
	progress(fmt.Sprintf(format, args...))
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
