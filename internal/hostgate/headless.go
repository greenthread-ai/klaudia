package hostgate

import (
	"context"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

// HeadlessApprover is the Approver for runs with no human present.
//
// The old behaviour was DenyAll for everything, which is wrong in two
// directions at once. Ordinary work in a CI job is exactly what an agent should
// be doing unattended, and denying it makes automation useless; host changes in
// a CI job are exactly what should not happen by accident, and denying them in
// silence teaches nobody why.
//
// So this splits the two. Non-host asks are refused as before — a headless run
// resolves those with --permission-mode and allow rules, not by asking. Host
// changes are refused with a message naming the flag that would permit them, so
// the run's output says what to do rather than just what failed.
//
// allowHost inverts the host half: --allow-host-changes on a machine the
// operator is willing to have reconfigured (a disposable CI image, a
// provisioning run). It is a deliberate, per-invocation opt-in and is never
// implied by any other flag.
func HeadlessApprover(allowHost bool) agent.Approver {
	return agent.ApproverFunc(func(_ context.Context, req agent.ApprovalRequest) permission.Decision {
		hc := CardOf(req)
		if hc == nil {
			msg := req.Suggestion
			if msg == "" {
				msg = "Tool " + req.ToolName + " requires approval, but no interactive approver is available."
			}
			return permission.Decision{Behavior: permission.Deny, Message: msg}
		}
		if allowHost {
			return permission.Decision{Behavior: permission.Allow}
		}
		what := strings.TrimSpace(hc.Summary)
		if what == "" {
			what = "this change"
		}
		return permission.Decision{
			Behavior: permission.Deny,
			Message: "no one is here to approve changes to this machine (" + what + "). " +
				"Re-run with --allow-host-changes if this machine is meant to be reconfigured. " +
				"For now, continue with the rest of the task without it and say what you could not do.",
		}
	})
}
