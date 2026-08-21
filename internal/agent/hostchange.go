package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// hostChangeApprover connects the RequestHostChange tool to the frontend and to
// the grant ledger.
//
// It lives here rather than in tools because the ledger does: a tool that could
// mint its own grants would be a tool that authorises itself. The tool asks;
// this decides what the answer buys.
type hostChangeApprover struct {
	gate     *HostGate
	approver Approver
}

// hostChangeFor returns the approver for a run, or nil when there is nothing to
// approve against. Nil is meaningful: the tool tells the model there is no one
// to ask, rather than reporting a failure it cannot act on.
func hostChangeFor(opts Options) tools.HostApprover {
	if opts.Host == nil || opts.Host.Ledger == nil || opts.Host.Policy == HostOff || opts.Host.Policy == "" {
		return nil
	}
	return hostChangeApprover{gate: opts.Host, approver: opts.Approver}
}

func (h hostChangeApprover) RequestHostChange(ctx context.Context, req tools.HostChangeRequest) (tools.HostChangeOutcome, error) {
	if h.gate == nil || h.gate.Ledger == nil {
		return tools.HostChangeOutcome{}, errors.New("no trust ledger in this session")
	}
	approver := h.approver
	if approver == nil {
		approver = DenyAll
	}

	// Validate the scope before showing it. A request naming /etc as a whole is
	// refused outright rather than put to the user: the answer to "may I have
	// all of /etc?" should not depend on how tired they are.
	if _, err := h.gate.Ledger.Preview(trust.Request{
		Summary: req.Summary, Reason: req.Reason,
		Paths: req.Paths, Services: req.Services, Packages: req.Packages,
	}); err != nil {
		return tools.HostChangeOutcome{
			Message: err.Error() + " Narrow the request to the specific files, services or " +
				"packages the operation needs and ask again.",
		}, nil
	}

	// Drift: the user has approved something already and this is a second ask.
	// Worth saying, because "this wasn't part of what you approved" is a
	// different question from the first one.
	drift := len(h.gate.Ledger.List()) > 0

	ad := approver.Approve(ctx, ApprovalRequest{
		ToolName:   "RequestHostChange",
		Suggestion: req.Summary,
		HostChange: &HostChange{
			Summary:  req.Summary,
			Reason:   req.Reason,
			Paths:    req.Paths,
			Services: req.Services,
			Packages: req.Packages,
			Declared: true,
			Drift:    drift,
			Zone:     trust.ZoneHost,
		},
	})
	if ad.Behavior != permission.Allow {
		msg := strings.TrimSpace(ad.Message)
		if msg == "" {
			return tools.HostChangeOutcome{}, nil // tool supplies the default wording
		}
		return tools.HostChangeOutcome{Message: "The user declined: " + msg +
			" Do not retry it or look for another way. Continue with the rest of the task without it."}, nil
	}

	g, err := h.gate.Ledger.Mint(trust.Request{
		Summary: req.Summary, Reason: req.Reason,
		Paths: req.Paths, Services: req.Services, Packages: req.Packages,
	})
	if err != nil {
		return tools.HostChangeOutcome{}, err
	}
	if h.gate.Granted != nil {
		h.gate.Granted(g)
	}
	return tools.HostChangeOutcome{Approved: true}, nil
}
