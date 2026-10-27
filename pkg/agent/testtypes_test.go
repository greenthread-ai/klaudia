package agent

import (
	"context"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/subagent"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/worktree"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
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

// testIsolator mirrors the CLI's gitIsolator (cli/isolator.go), which tests
// here cannot import: a git worktree each, or none.
func testIsolator(on bool) Isolator {
	if !on {
		return nil
	}
	return gitTestIsolator{}
}

type gitTestIsolator struct{}

func (gitTestIsolator) Isolate(ctx context.Context, root, label string) (Workspace, error) {
	if !worktree.Supported(ctx, root) {
		return nil, nil
	}
	t, err := worktree.New(ctx, root, label)
	if err != nil {
		return nil, err
	}
	return gitTestWorkspace{t}, nil
}

type gitTestWorkspace struct{ t *worktree.Tree }

func (w gitTestWorkspace) Dir() string                      { return w.t.Dir }
func (w gitTestWorkspace) Rewrite(s string) string          { return w.t.Rewrite(s) }
func (w gitTestWorkspace) Remove(ctx context.Context) error { return w.t.Remove(ctx) }

func (w gitTestWorkspace) Adopt(ctx context.Context) (AdoptResult, error) {
	rep, err := w.t.Adopt(ctx)
	return AdoptResult{Summary: rep.Summary(), Conflicted: len(rep.Conflicted) > 0, Empty: rep.Empty()}, err
}
