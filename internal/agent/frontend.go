package agent

import (
	"context"
	"encoding/json"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// The agent core is UI-agnostic. A frontend (headless CLI, Bubble Tea TUI, an
// editor integration such as Zed/ACP, or an SDK embedding) drives the loop via
// Run and consumes its Emitter event stream. The one decision the core cannot
// make on its own is how to resolve a permission "ask" — that is delegated to
// the frontend via an Approver.

// ApprovalRequest describes a tool invocation awaiting approval. It carries
// everything a frontend needs to decide or prompt: the tool name, the raw
// input, the rule-matchable specifier, and the suggested message.
type ApprovalRequest struct {
	ToolName  string
	ToolUseID string
	Input     json.RawMessage
	Specifier string
	// Suggestion is the message from the intrinsic check (may be empty).
	Suggestion string
	// HostChange, when set, means this approval is about a change to the
	// machine Klaudia is running on rather than about a tool's ordinary
	// permission. Frontends should render it as the host-change card — what
	// changes and why — because "allow Bash?" is the wrong question to put to
	// someone about `systemctl restart nginx`.
	HostChange *HostChange
}

// HostChange describes a change to this machine for the approval UI.
type HostChange struct {
	// Summary is the human phrasing of what changes: the model's own words when
	// it declared the operation, the classifier's when it was caught.
	Summary string
	// Reason is why the task needs it. Only set for a declared change — the
	// classifier can say what a command does but not what it is for.
	Reason string
	Zone   trust.Zone
	// Effects is what the classifier found. Empty for a declared change, which
	// has not happened yet.
	Effects []trust.Effect
	// Paths, Services and Packages are the scope the model declared.
	Paths    []string
	Services []string
	Packages []string
	// Declared distinguishes "the model asked first" from "this was caught on
	// the way past". The first is the flow working; the second is the fallback.
	Declared bool
	// Drift is true when the user has already approved something and this falls
	// outside it. Worth saying plainly: "this wasn't part of what you approved"
	// is a different question from the first one.
	Drift bool
}

// Approver resolves a permission "ask" for a tool invocation. It returns a
// decision of Allow or Deny (Ask is treated as Deny). Implementations:
//   - headless: deny (no one to ask), or apply an auto-policy
//   - TUI: prompt the user and return their choice
//   - editor/SDK: forward the request to the client and await the answer
//
// Approver must respect ctx cancellation.
type Approver interface {
	Approve(ctx context.Context, req ApprovalRequest) permission.Decision
}

// ApproverFunc adapts a function to the Approver interface.
type ApproverFunc func(ctx context.Context, req ApprovalRequest) permission.Decision

func (f ApproverFunc) Approve(ctx context.Context, req ApprovalRequest) permission.Decision {
	return f(ctx, req)
}

// DenyAll is the default Approver: it denies every "ask" with an explanatory
// message. Used by headless runs with no interactive frontend. The model adapts
// and reports without performing the action.
var DenyAll Approver = ApproverFunc(func(_ context.Context, req ApprovalRequest) permission.Decision {
	msg := req.Suggestion
	if msg == "" {
		msg = "Tool " + req.ToolName + " requires approval, but no interactive approver is available."
	}
	return permission.Decision{Behavior: permission.Deny, Message: msg}
})
