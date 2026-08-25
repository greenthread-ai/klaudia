package tui

import (
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// The host-change card.
//
// The question a permission prompt normally asks is "allow Bash(sudo apt-get
// install -y nginx)?" — a question about a command line, which the user has to
// reverse-engineer into an intention before they can answer it. This card asks
// about the intention instead: what changes, why the task needs it, and how far
// the agreement reaches.
//
// The reach is the part worth being careful about. A user who approves
// "configure nginx" and then discovers it also covered /etc/hosts has been
// misled, so the card lists the scope in full rather than summarising it, and
// says plainly that the approval lasts for this session only.

// hostCardLines renders the approval card as scrollback lines.
func hostCardLines(hc *agent.HostChange) []string {
	if hc == nil {
		return nil
	}
	var out []string
	head := "This changes your machine"
	if hc.Drift {
		head = "This wasn't part of what you approved"
	}
	out = append(out, askStyle.Render(head))

	if s := strings.TrimSpace(hc.Summary); s != "" {
		out = append(out, "  "+s)
	}
	if r := strings.TrimSpace(hc.Reason); r != "" {
		out = append(out, toolStyle.Render("  why: "+r))
	}

	for _, line := range hostScopeLines(hc) {
		out = append(out, toolStyle.Render("  "+line))
	}

	if hc.Declared {
		out = append(out, bannerStyle.Render(
			"  approving covers every step inside that scope, for this session only"))
	} else {
		// Not declared means the model went ahead and the classifier caught it.
		// Worth flagging: it is the difference between being asked and being
		// stopped, and a user seeing it often is seeing a model that is not
		// using RequestHostChange.
		out = append(out, bannerStyle.Render(
			"  caught on the way past — Klaudia did not declare this first"))
	}
	return out
}

// hostScopeLines lists what the approval reaches, one category per line.
func hostScopeLines(hc *agent.HostChange) []string {
	var out []string
	add := func(label string, items []string) {
		if len(items) > 0 {
			out = append(out, label+": "+strings.Join(items, ", "))
		}
	}
	add("paths", hc.Paths)
	add("services", hc.Services)
	add("packages", hc.Packages)

	// A caught change has effects rather than a declared scope.
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
		out = append(out, "found: "+strings.Join(found, "; "))
	}
	return out
}

// hostBlockedLine turns the gate's message to the model into the one line the
// user needs.
//
// The model is told what was found, that another route is preferred, and how to
// declare the change if it has to happen. The user needs none of that: they
// need to know Klaudia tried something, was not allowed, and is moving on. The
// rest is instruction addressed to somebody else, and printing it was what made
// a routine redirect read as a wall of policy.
func hostBlockedLine(msg string) string {
	what := strings.TrimSpace(msg)
	if i := strings.Index(what, ". "); i > 0 {
		what = what[:i]
	}
	what = strings.TrimSuffix(strings.TrimSpace(what), ".")
	if what == "" {
		return "not allowed — trying another way"
	}
	return strings.ToLower(what[:1]) + what[1:] + " — trying another way"
}

// hostPrompt is the actionable line in the persistent bottom view.
//
// There is no "always" here, deliberately. An always-allow on a host change
// would be a permission the user cannot see and did not schedule the end of;
// approving an operation is the durable answer this model offers, and it lasts
// for the session.
//
// There is a third answer, though, and it is not a refusal. "No" ends the
// attempt; "something else" keeps the turn alive and lets the user redirect —
// which is usually what someone means when they decline a host change. They
// rarely want the task abandoned, they want it done differently.
func hostPrompt(hc *agent.HostChange) string {
	verb := "Change this machine?"
	if hc != nil && hc.Drift {
		verb = "Approve this too?"
	}
	return verb + " (y)es / (n)o / (s)omething else"
}

// hostAnswerLine echoes the decision with its reach, so the scrollback records
// what was agreed to and not merely that something was.
func hostAnswerLine(hc *agent.HostChange, allowed, redirect bool) string {
	if redirect {
		return "declined — say what you'd like instead, and it lands before Klaudia's next step"
	}
	if !allowed {
		return "declined — Klaudia will carry on without it"
	}
	scope := strings.Join(hostScopeLines(hc), "; ")
	if scope == "" {
		return "approved for this session"
	}
	return "approved for this session — " + scope
}

// hostGrantLine reports what an approval actually bought, which is not always
// what was asked for: the scope is widened to the operation and bounded away
// from anything that would hand over a system directory.
func hostGrantLine(g *trust.Grant) string {
	if g == nil {
		return ""
	}
	return fmt.Sprintf("approved · %s", g.Describe())
}

// hostReportLine renders one classifier finding for observe mode and /trust.
func hostReportLine(r agent.HostReport) string {
	state := "would ask"
	switch {
	case r.Enforced:
		state = "stopped"
	case r.Covered:
		state = "covered by an approval"
	}
	summary := r.Summary
	if summary == "" {
		summary = r.Zone.String()
	}
	return fmt.Sprintf("%s · %s · %s", r.Tool, summary, state)
}
