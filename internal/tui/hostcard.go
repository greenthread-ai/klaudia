package tui

import (
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
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
func hostCardLines(hc *hostgate.HostChange) []string {
	if hc == nil {
		return nil
	}
	var out []string
	head := "This changes your machine"
	switch {
	case hc.RepoConfig != "":
		head = "This repository wants to run its own commands"
	case hc.Drift:
		head = "This wasn't part of what you approved"
	}
	out = append(out, askStyle.Render(head))

	// Summary and commands come from the model or a repository's config:
	// shown with nothing hidden (visible), as the plain approval is.
	if s := strings.TrimSpace(hc.Summary); s != "" {
		out = append(out, "  "+visible(s))
	}
	if r := strings.TrimSpace(hc.Reason); r != "" {
		out = append(out, toolStyle.Render("  why: "+visible(r)))
	}

	for _, line := range hostScopeLines(hc) {
		out = append(out, toolStyle.Render("  "+line))
	}

	switch {
	case hc.RepoConfig != "":
		// Not "for this session only", because it is not. Saying so about a
		// decision that outlives the session would be a lie in the one place
		// the user is deciding how far to trust a repository.
		out = append(out, bannerStyle.Render(
			"  remembered until these "+hc.RepoConfig+" change; editing any of them asks again"))
	case hc.Declared:
		out = append(out, bannerStyle.Render(
			"  approving covers every step inside that scope, for this session only"))
	default:
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
func hostScopeLines(hc *hostgate.HostChange) []string {
	var out []string
	add := func(label string, items []string) {
		if len(items) > 0 {
			out = append(out, label+": "+visible(strings.Join(items, ", ")))
		}
	}
	add("paths", hc.Paths)
	add("services", hc.Services)
	add("packages", hc.Packages)
	// One per line, not joined: a command line contains commas, and a list of
	// them run together is exactly the thing a user skims instead of reading.
	for _, c := range hc.Commands {
		out = append(out, "  "+visible(c))
	}

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
func hostPrompt(hc *hostgate.HostChange) string {
	verb := "Change this machine?"
	switch {
	case hc != nil && hc.RepoConfig == hostgate.RepoMCP:
		return "Start these MCP servers? (y)es / (n)o"
	case hc != nil && hc.RepoConfig != "":
		// No "something else" offered. That answer exists to redirect the model
		// mid-task; there is no task here to redirect, only a yes or a no about
		// a config file.
		return "Run these " + hc.RepoConfig + "? (y)es / (n)o"
	case hc != nil && hc.Drift:
		verb = "Approve this too?"
	}
	return verb + " (y)es / (n)o / (s)omething else"
}

// hostAnswerLine echoes the decision with its reach, so the scrollback records
// what was agreed to and not merely that something was.
func hostAnswerLine(hc *hostgate.HostChange, allowed, redirect bool) string {
	if redirect {
		return "declined — say what you'd like instead, and it lands before Klaudia's next step"
	}
	if hc != nil && hc.RepoConfig != "" {
		// A repository's config, not a step of the task: there is nothing to
		// carry on without, and the yes is not session-scoped.
		if allowed {
			return "approved — remembered until these " + hc.RepoConfig + " change"
		}
		return "declined — these " + hc.RepoConfig + " will not run this session"
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

// hostReportLine renders one classifier finding for observe mode and /trust.
func hostReportLine(r hostgate.HostReport) string {
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
