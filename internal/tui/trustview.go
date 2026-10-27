package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/trust"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// /trust answers three questions, and it exists because none of them had an
// answer before: what is Klaudia allowed to do to this machine, what has it
// already been allowed to do, and what has it tried.
//
// There is no posture to set here any more. The gate always classifies, so the
// subcommands are about approvals rather than about whether checking happens:
// `/mode bypassPermissions` is the way to stop checking, and it says so.

// TrustController lets /trust inspect the session's host guardrail and manage
// its approvals, without the TUI owning the gate.
type TrustController interface {
	Grants() []*trust.Grant
	Reports() []hostgate.HostReport
	// Covers reports whether live grants already authorise these effects. The
	// completion block uses it to avoid calling a change "not done" when the
	// model declared it properly a moment later and the user said yes.
	Covers([]trust.Effect) bool
	Revoke(id string) bool
	RevokeAll() int
}

// gateController adapts a HostGate to TrustController.
type gateController struct{ gate *hostgate.HostGate }

// NewTrustController wraps a host gate for the TUI. Returns nil when there is
// no gate, which /trust reports rather than hiding.
func NewTrustController(g *hostgate.HostGate) TrustController {
	if g == nil {
		return nil
	}
	return gateController{gate: g}
}

func (c gateController) Grants() []*trust.Grant         { return c.gate.Grants() }
func (c gateController) Reports() []hostgate.HostReport { return c.gate.Reports() }
func (c gateController) Covers(e []trust.Effect) bool {
	if c.gate == nil || c.gate.Ledger == nil || len(e) == 0 {
		return false
	}
	_, drift := c.gate.Ledger.Cover(e)
	return len(drift) == 0
}
func (c gateController) Revoke(id string) bool { return c.gate.Ledger.Revoke(id) }
func (c gateController) RevokeAll() int        { return c.gate.Ledger.RevokeAll() }

// renderTrust is the /trust view.
func (m *Model) renderTrust() string {
	tc := m.sess.Trust
	var b strings.Builder

	if tc == nil {
		b.WriteString("Host guardrail: not available in this session.\n")
		b.WriteString(trustHonesty)
		return b.String()
	}

	if m.currentMode() == permission.ModeBypassPermissions {
		b.WriteString("Host guardrail: BYPASSED — the mode skips it entirely; nothing below is being checked.\n")
	} else {
		b.WriteString("Host guardrail: enforcing — Klaudia asks before changing this machine.\n")
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
func trustReportSummary(reports []hostgate.HostReport) []string {
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
	case "revoke":
		if len(args) < 2 {
			m.appendLine(errStyle.Render("/trust revoke <id> or /trust revoke all"))
			return
		}
		if strings.EqualFold(args[1], "all") {
			n := tc.RevokeAll()
			m.appendLine(bannerStyle.Render(fmt.Sprintf("Revoked %s. "+
				"Klaudia will ask again before the next host change.", countOf(n, "approval"))))
			return
		}
		if tc.Revoke(args[1]) {
			m.appendLine(bannerStyle.Render("Revoked " + args[1] + "."))
			return
		}
		m.appendLine(errStyle.Render("no live approval with id " + args[1]))

	default:
		m.appendLine(errStyle.Render("/trust [revoke <id>|revoke all]"))
	}
}
