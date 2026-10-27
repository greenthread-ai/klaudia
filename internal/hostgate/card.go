package hostgate

import (
	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

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
	// Commands are command lines the approval would permit, listed verbatim.
	// Unlike the three scopes above these are not summarised or joined: the
	// whole value of showing a command is that the user can read it.
	Commands []string
	// Hooks marks the one question on this card that is not about reconfiguring
	// the machine: whether the shell commands a repository declared may run
	// around Klaudia's tool calls. It shares the card because the decision has
	// the same shape — unconfined execution the user has to agree to — but three
	// of the framing sentences differ. A hook set is not "caught on the way
	// past" (the model had nothing to do with it), and the answer is remembered
	// rather than session-scoped.
	Hooks bool
	// Declared distinguishes "the model asked first" from "this was caught on
	// the way past". The first is the flow working; the second is the fallback.
	Declared bool
	// Drift is true when the user has already approved something and this falls
	// outside it. Worth saying plainly: "this wasn't part of what you approved"
	// is a different question from the first one.
	Drift bool
}

// Fields renders a host change as the flat map an out-of-process frontend puts
// on the wire: stream-json's host_change payload and ACP's _meta both use it.
//
// It lives on the type rather than in either frontend because the omissions are
// the interesting part and they have to agree. Only populated fields are
// included: a declared change has a Reason and no Effects, a caught one the
// reverse, and a peer should be able to tell which it has. An empty scope is
// absent rather than an empty array, so "no services" and "services it did not
// say" stay distinguishable. Effects are rendered with Effect.Describe — the
// same phrasing the TUI's host card shows — so a peer displays what a terminal
// user would read rather than a struct dump.
func (hc *HostChange) Fields() map[string]any {
	if hc == nil {
		return nil
	}
	m := map[string]any{
		"summary":  hc.Summary,
		"zone":     hc.Zone.String(),
		"declared": hc.Declared,
	}
	if hc.Reason != "" {
		m["reason"] = hc.Reason
	}
	if len(hc.Effects) > 0 {
		seen := map[string]bool{}
		var found []string
		for _, e := range hc.Effects {
			d := e.Describe()
			if !seen[d] {
				seen[d] = true
				found = append(found, d)
			}
		}
		m["effects"] = found
	}
	if len(hc.Paths) > 0 {
		m["paths"] = hc.Paths
	}
	if len(hc.Services) > 0 {
		m["services"] = hc.Services
	}
	if len(hc.Packages) > 0 {
		m["packages"] = hc.Packages
	}
	if len(hc.Commands) > 0 {
		m["commands"] = hc.Commands
	}
	if hc.Hooks {
		m["hooks"] = true
	}
	if hc.Drift {
		m["drift"] = true
	}
	return m
}

// CardOf returns the host change an approval request is about, or nil when it
// is an ordinary permission ask. The gate puts its card in the request's
// Detail; frontends render it from there.
func CardOf(req agent.ApprovalRequest) *HostChange {
	hc, _ := req.Detail.(*HostChange)
	return hc
}
