package cli

import (
	"context"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/hooks"
)

// loopHooks adapts Klaudia's hook runner to the loop's agent.Hooks. The loop
// decides when hooks fire and what their results mean; internal/hooks decides
// which hooks exist, whether a project's may run, and how to run them. This is
// the seam, in the wiring, so neither package imports the other.
type loopHooks struct{ r *hooks.Runner }

// hooksFor returns nil for a nil runner — never an interface holding a nil
// pointer, which the loop would take for a real source of hooks.
func hooksFor(r *hooks.Runner) agent.Hooks {
	if r == nil {
		return nil
	}
	return loopHooks{r}
}

func (h loopHooks) Has(ev agent.HookEvent) bool { return h.r.Has(hooks.Event(ev)) }
func (h loopHooks) ClaimSessionStart() bool     { return h.r.ClaimSessionStart() }

func (h loopHooks) Run(ctx context.Context, in agent.HookInput, confirm agent.HookConfirm) agent.HookResult {
	var c hooks.Confirm
	if confirm != nil {
		c = func(ctx context.Context, hs []hooks.Hook, file string) bool {
			lines := make([]string, len(hs))
			for i, hk := range hs {
				lines[i] = hk.String()
			}
			return confirm(ctx, lines, file)
		}
	}
	res := h.r.Run(ctx, hooks.Input{
		Event: hooks.Event(in.Event), CWD: in.CWD,
		ToolName: in.ToolName, ToolInput: in.ToolInput, ToolResult: in.ToolResult, ToolError: in.ToolError,
		Prompt: in.Prompt,
	}, c)
	return agent.HookResult{Blocked: res.Blocked, Reason: res.Reason, Context: res.Context, Notices: res.Notices}
}
