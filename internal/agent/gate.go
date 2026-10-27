package agent

import (
	"context"

	tools "github.com/greenthread-ai/klaudia/pkg/tool"
)

// Gate is a policy every tool call passes before the permission check, and
// the only part of a call's fate the loop does not decide itself.
//
// Order is the one non-negotiable: the permission check is per-tool and
// mode-driven, and autonomous — the default — allows every tool it is asked
// about, so a gate running second would never see the calls it exists for.
// bypassPermissions skips it, because that mode's whole contract is "no
// checks".
//
// The loop does not know what a gate checks. Klaudia's (internal/hostgate)
// stops changes to the machine it runs on that the user has not agreed to — a
// harness running on someone's own host needs that. A host embedding the loop
// in a sandbox has nothing to protect that way and leaves Options.Gate nil,
// which is no gate at all.
type Gate interface {
	// Check decides one call. It may ask the user through call.Approver.
	Check(ctx context.Context, call GateCall) GateVerdict
	// WouldAsk reports whether Check would need to ask the user, without
	// asking — so such a call is kept out of a parallel batch, where a
	// question would interleave with other calls' output.
	WouldAsk(tool string, input []byte, cwd string) bool
	// HostChanges backs a declaration tool (tools.Context.HostChange) for
	// this run, or returns nil when there is nothing to declare against.
	HostChanges(approver Approver) tools.HostApprover
}

// GateCall is one tool call put to a Gate.
type GateCall struct {
	Tool       string
	ToolUseID  string
	Input      []byte
	WorkingDir string
	// Approver is the frontend's, for a gate that needs to ask.
	Approver Approver
}

// GateVerdict is a Gate's decision.
type GateVerdict struct {
	// Allow lets the call proceed to the permission check.
	Allow bool
	// Approved means the user agreed to this call just now. Anything the loop
	// was holding against the tool from earlier refusals is stale.
	Approved bool
	// Refuse is the tool_result the model gets when Allow is false: what
	// stopped the call and what to do instead.
	Refuse string
}
