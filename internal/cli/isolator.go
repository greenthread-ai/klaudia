package cli

import (
	"context"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/worktree"
)

// gitIsolator gives the spawner's writing sub-agents a git worktree each
// (agent.Isolator): the spawner decides when a child needs one, the worktree
// package knows how to make, land and remove it, and this is the seam between
// them — here, in the wiring, so neither package imports the other.
type gitIsolator struct{}

// isolator is the configured choice: a worktree each, or nothing when
// [subagents] worktree = false.
func isolator(on bool) agent.Isolator {
	if !on {
		return nil
	}
	return gitIsolator{}
}

func (gitIsolator) Isolate(ctx context.Context, root, label string) (agent.Workspace, error) {
	if !worktree.Supported(ctx, root) {
		return nil, nil
	}
	t, err := worktree.New(ctx, root, label)
	if err != nil {
		return nil, err
	}
	return gitWorkspace{t}, nil
}

type gitWorkspace struct{ t *worktree.Tree }

func (w gitWorkspace) Dir() string                      { return w.t.Dir }
func (w gitWorkspace) Rewrite(s string) string          { return w.t.Rewrite(s) }
func (w gitWorkspace) Remove(ctx context.Context) error { return w.t.Remove(ctx) }

func (w gitWorkspace) Adopt(ctx context.Context) (agent.AdoptResult, error) {
	rep, err := w.t.Adopt(ctx)
	return agent.AdoptResult{Summary: rep.Summary(), Conflicted: len(rep.Conflicted) > 0, Empty: rep.Empty()}, err
}
