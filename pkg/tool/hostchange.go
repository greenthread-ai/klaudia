package tool

import "context"

// HostChangeRequest is the declared change, passed to the frontend.
type HostChangeRequest struct {
	Summary  string
	Reason   string
	Paths    []string
	Services []string
	Packages []string
}

// HostChangeOutcome is the frontend's answer.
type HostChangeOutcome struct {
	Approved bool
	// Message, when set, replaces the default text sent back to the model —
	// a frontend can explain a refusal in the user's own words.
	Message string
}

// HostApprover puts a declared host change to the user and records the grant it
// buys. Implemented by the host — Klaudia's is internal/hostgate, which owns the grant
// ledger — and handed to tools as Context.HostChange.
type HostApprover interface {
	RequestHostChange(ctx context.Context, req HostChangeRequest) (HostChangeOutcome, error)
}
