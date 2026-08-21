package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// /trust answers three questions, and it exists because none of them had an
// answer before: what is Klaudia allowed to do to this machine, what has it
// already been allowed to do, and what has it tried.
//
// The third matters most in observe mode, which reports without enforcing.
// Without a view, "observe" is indistinguishable from "off" — the classifier
// would be running and telling nobody, which is worse than not running.

// TrustController lets /trust inspect and change the session's host guardrail
// without the TUI owning the gate.
type TrustController interface {
	Policy() agent.HostPolicy
	SetPolicy(agent.HostPolicy)
	Grants() []*trust.Grant
	Reports() []agent.HostReport
	Revoke(id string) bool
	RevokeAll() int
}

// gateController adapts a HostGate to TrustController.
type gateController struct{ gate *agent.HostGate }

// NewTrustController wraps a host gate for the TUI. Returns nil when there is
// no gate, which /trust reports rather than hiding.
func NewTrustController(g *agent.HostGate) TrustController {
	if g == nil {
		return nil
	}
	return gateController{gate: g}
}

func (c gateController) Policy() agent.HostPolicy     { return c.gate.Policy }
func (c gateController) SetPolicy(p agent.HostPolicy) { c.gate.Policy = p }
func (c gateController) Grants() []*trust.Grant       { return c.gate.Grants() }
func (c gateController) Reports() []agent.HostReport  { return c.gate.Reports() }
func (c gateController) Revoke(id string) bool        { return c.gate.Ledger.Revoke(id) }
func (c gateController) RevokeAll() int               { return c.gate.Ledger.RevokeAll() }

// renderTrust is the /trust view.
func (m *Model) renderTrust() string {
	tc := m.sess.Trust
	var b strings.Builder

	if tc == nil {
		b.WriteString("Host guardrail: not available in this session.\n")
		b.WriteString(trustHonesty)
		return b.String()
	}

	switch tc.Policy() {
	case agent.HostEnforce:
		b.WriteString("Host guardrail: enforcing — Klaudia asks before changing this machine.\n")
	case agent.HostObserve:
		b.WriteString("Host guardrail: observing — findings are listed below, but nothing is stopped.\n")
		b.WriteString("  /trust upgrade switches to enforcing and drops per-action prompts.\n")
	case agent.HostOff:
		b.WriteString("Host guardrail: off — nothing below is being checked.\n")
	}
	fmt.Fprintf(&b, "Mode: %s\n", m.currentMode().Label())

	grants := tc.Grants()
	b.WriteString("\nApprovals this session")
	if len(grants) == 0 {
		b.WriteString(": none.\n")
	} else {
		b.WriteString(":\n")
		for _, g := range grants {
			fmt.Fprintf(&b, "  %-4s %s\n", g.ID, g.Describe())
			if g.Reason != "" {
				fmt.Fprintf(&b, "       why: %s\n", g.Reason)
			}
		}
		b.WriteString("  /trust revoke <id> or /trust revoke all\n")
	}

	if reports := tc.Reports(); len(reports) > 0 {
		b.WriteString("\nWhat the classifier found:\n")
		for _, line := range trustReportSummary(reports) {
			b.WriteString("  " + line + "\n")
		}
	}

	if legacy := m.legacyRuleSummary(); legacy != "" {
		b.WriteString("\n" + legacy)
	}

	b.WriteString("\n" + trustHonesty)
	return b.String()
}

// trustHonesty is the one paragraph in this feature that must not oversell.
//
// The guardrail reads command lines and tool inputs; it does not watch
// syscalls. A command that computes its own target, a script Klaudia wrote a
// moment ago, or a package postinstall hook all go past it. Saying so plainly
// is the difference between a user calibrating their trust correctly and a user
// discovering the limits the hard way. The wording is asserted by a test.
const trustHonesty = "Klaudia asks before host changes it can detect, by reading commands and tool inputs.\n" +
	"It does not watch what programs actually do, so a command that builds its own\n" +
	"target, or a package's install script, can change things without being seen.\n" +
	"For enforcement the kernel applies, set sandbox mode to \"os\" in .klaudia/config.toml."

// trustReportSummary collapses the findings into one line per distinct summary,
// newest last, with a count. A long session produces the same finding many
// times and a scrolling list of duplicates is unreadable.
func trustReportSummary(reports []agent.HostReport) []string {
	type entry struct {
		line  string
		count int
		order int
	}
	seen := map[string]*entry{}
	for i, r := range reports {
		key := hostReportLine(r)
		if e, ok := seen[key]; ok {
			e.count++
			continue
		}
		seen[key] = &entry{line: key, count: 1, order: i}
	}
	out := make([]*entry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].order < out[j].order })
	lines := make([]string, 0, len(out))
	for _, e := range out {
		if e.count > 1 {
			lines = append(lines, fmt.Sprintf("%s (×%d)", e.line, e.count))
			continue
		}
		lines = append(lines, e.line)
	}
	return lines
}

// legacyRuleSummary reports rules carried over from the per-command model.
//
// They still work, and Klaudia no longer creates them. Listing them here is how
// a user finds out they have accumulated a pile of approvals from before the
// zone model existed.
func (m *Model) legacyRuleSummary() string {
	if len(m.sessionAllow) == 0 && len(m.sessionDeny) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Rules from the per-command model (still honoured; no new ones are created):\n")
	write := func(kind string, rules []permission.Rule) {
		for _, r := range rules {
			spec := r.Specifier
			if spec != "" {
				spec = "(" + spec + ")"
			}
			fmt.Fprintf(&b, "  %s %s%s\n", kind, r.Tool, spec)
		}
	}
	write("allow", m.sessionAllow)
	write("deny", m.sessionDeny)
	return b.String()
}

// modeRefusal reports why a mode cannot be selected, or "" if it can.
//
// Autonomous relies on the host gate to be the thing that stops a host change.
// Without an enforcing gate it is bypassPermissions with a reassuring name,
// and offering it in that state would be the single most misleading thing this
// feature could do.
func (m *Model) modeRefusal(want permission.Mode) string {
	if want != permission.ModeAutonomous {
		return ""
	}
	if m.sess.Trust == nil {
		return "autonomous needs the host guardrail, which this session does not have"
	}
	if p := m.sess.Trust.Policy(); p != agent.HostEnforce {
		return fmt.Sprintf("autonomous needs the host guardrail enforcing (it is %s). "+
			"Run /trust upgrade first — without it, autonomous would allow everything.", p)
	}
	return ""
}

// trustCommand handles /trust and its subcommands.
func (m *Model) trustCommand(args []string) {
	tc := m.sess.Trust
	if len(args) == 0 {
		m.appendLine(bannerStyle.Render(m.renderTrust()))
		return
	}
	if tc == nil {
		m.appendLine(errStyle.Render("no host guardrail in this session"))
		return
	}

	switch strings.ToLower(args[0]) {
	case "upgrade":
		if tc.Policy() == agent.HostEnforce {
			m.appendLine(bannerStyle.Render("Already enforcing."))
			return
		}
		tc.SetPolicy(agent.HostEnforce)
		m.sess.PermissionMode = string(permission.ModeAutonomous)
		m.appendLine(bannerStyle.Render(
			"Enforcing. Klaudia now works without per-action prompts inside the project " +
				"and asks before changing this machine.\n" +
				"Your existing rules still apply. Add [trust] mode = \"enforce\" to " +
				".klaudia/config.toml to keep this next session."))

	case "off":
		// Turning the guardrail off while autonomous would leave nothing at all
		// between the model and the machine, so the mode goes back with it.
		tc.SetPolicy(agent.HostOff)
		if m.currentMode() == permission.ModeAutonomous {
			m.sess.PermissionMode = string(permission.ModeDefault)
			m.appendLine(bannerStyle.Render(
				"Host guardrail off. Permission mode is back to asking per action — " +
					"autonomous without the guardrail would allow everything."))
			return
		}
		m.appendLine(bannerStyle.Render("Host guardrail off for this session."))

	case "observe":
		tc.SetPolicy(agent.HostObserve)
		if m.currentMode() == permission.ModeAutonomous {
			m.sess.PermissionMode = string(permission.ModeDefault)
		}
		m.appendLine(bannerStyle.Render("Observing: findings are recorded, nothing is stopped. /trust to see them."))

	case "revoke":
		if len(args) < 2 {
			m.appendLine(errStyle.Render("/trust revoke <id> or /trust revoke all"))
			return
		}
		if strings.EqualFold(args[1], "all") {
			n := tc.RevokeAll()
			m.appendLine(bannerStyle.Render(fmt.Sprintf("Revoked %d approval(s). "+
				"Klaudia will ask again before the next host change.", n)))
			return
		}
		if tc.Revoke(args[1]) {
			m.appendLine(bannerStyle.Render("Revoked " + args[1] + "."))
			return
		}
		m.appendLine(errStyle.Render("no live approval with id " + args[1]))

	default:
		m.appendLine(errStyle.Render("/trust [upgrade|observe|off|revoke <id>|revoke all]"))
	}
}
