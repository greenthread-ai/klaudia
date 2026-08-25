package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// The host gate sits in front of every tool call, before the allow/deny rules.
//
// Order matters, and this is the one placement decision in the whole feature
// that is not negotiable. A user's allow rule says "Bash(git status:*) is fine";
// it does not say "and therefore anything I can get a matching prefix on is
// fine". If the gate ran after rule matching, an allow rule would launder a
// command line, and the protection would be defeatable by a prefix. So the gate
// runs first, and rules can only narrow what it permits.
//
// It also keeps internal/permission a leaf package. permission is imported by
// both tools and agent; giving it a dependency on trust — which needs a
// filesystem view and a session-scoped ledger — would have inverted that.
//
// # What this buys, and what it does not
//
// This reads tool inputs. It does not observe syscalls. A command that computes
// its own target, a script Klaudia wrote a moment ago, a package postinstall
// hook: all of them do whatever they do without this gate noticing. It is a
// guardrail against well-intentioned mistakes, and the honest description of
// its strength is "Klaudia asks before host changes it can detect". The real
// boundary is internal/sandbox, which is kernel enforcement and off by default.

// HostPolicy is how the gate behaves when it finds a change to this machine.
type HostPolicy string

const (
	// HostEnforce refuses undeclared host changes. The default.
	HostEnforce HostPolicy = "enforce"
	// HostObserve classifies and reports but changes no decisions. This is the
	// migration setting for sessions that already carry allow/deny rules: the
	// user can see what the new model *would* do for a while before it starts
	// doing it.
	HostObserve HostPolicy = "observe"
	// HostOff disables classification entirely.
	HostOff HostPolicy = "off"
)

// HostGate classifies tool calls and checks them against the session's grants.
// The zero value is inert, so a caller that has not wired trust up gets the old
// behaviour rather than a panic.
type HostGate struct {
	Policy HostPolicy
	// Roots is a function, not a value, for the same reason
	// permission.Context.Mode is: a session that adds a directory with /add-dir
	// should start treating it as project work on the next tool call, not at
	// the next turn boundary.
	Roots  func() trust.Roots
	Ledger *trust.Ledger
	// DeclareTool names the tool the model should call to describe a host
	// change before making it. When empty, the gate falls back to putting the
	// change directly to the user through the Approver.
	DeclareTool string
	// Observed, if set, is called for every call that raised a concern —
	// including the ones that were allowed. This is what feeds `/trust` and the
	// observe-mode reporting, and it is deliberately called for allowed calls
	// too: "a grant covered this" is the interesting half.
	Observed func(HostReport)
	// Granted, if set, is called when an approval mints a grant. The UI uses it
	// to show what the approval actually bought — the widened scope, not the
	// words the model used.
	Granted func(*trust.Grant)

	mu      sync.Mutex
	reports []HostReport
}

// maxHostReports bounds the in-memory log. It exists so /trust can show what
// the classifier found, especially in observe mode where nothing else does;
// a session that runs for hours should not accumulate it without limit.
const maxHostReports = 200

func (g *HostGate) record(r HostReport) {
	g.mu.Lock()
	g.reports = append(g.reports, r)
	if len(g.reports) > maxHostReports {
		g.reports = g.reports[len(g.reports)-maxHostReports:]
	}
	g.mu.Unlock()
	if g.Observed != nil {
		g.Observed(r)
	}
}

// noteDeclined records a declared host change that was refused.
//
// Without this the ledger only knows about changes it *caught* — a model that
// did the right thing, declared its intent up front and was told no, left no
// trace at all. That is the common path now, and it is the one an automation
// needs to see: /trust should show it, and a headless run should be able to
// exit differently because of it.
func (g *HostGate) noteDeclined(summary string) {
	if g == nil {
		return
	}
	g.record(HostReport{
		Tool: "RequestHostChange", Zone: trust.ZoneHost,
		Summary: summary, Enforced: true,
	})
}

// Reports returns what the classifier has found this session, oldest first.
func (g *HostGate) Reports() []HostReport {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]HostReport(nil), g.reports...)
}

// Grants returns the live grants, for /trust.
func (g *HostGate) Grants() []*trust.Grant {
	if g == nil || g.Ledger == nil {
		return nil
	}
	return g.Ledger.List()
}

// HostReport is one gate decision, for display.
type HostReport struct {
	Tool     string
	Zone     trust.Zone
	Summary  string
	Effects  []trust.Effect
	Covered  bool   // a live grant authorised it
	Enforced bool   // the call was actually stopped
	GrantID  string // the grant that covered it, when Covered
}

// HostDecision is what the gate concluded.
type HostDecision struct {
	// Allow is true when the call may proceed untouched.
	Allow bool
	// Refuse, when set, is the tool_result text explaining what to do instead.
	Refuse string
	// Ask, when non-empty, means the change should be put to the user directly:
	// no declaration tool is registered to route it through.
	Ask []trust.Effect
	// Assessment is the full classification, for the UI.
	Assessment trust.Assessment
}

// Check classifies a tool call and decides whether it may proceed.
func (g *HostGate) Check(tool string, input []byte, cwd string) HostDecision {
	if g == nil || g.Policy == HostOff || g.Policy == "" {
		return HostDecision{Allow: true}
	}

	var roots trust.Roots
	if g.Roots != nil {
		roots = g.Roots()
	}
	as := trust.ClassifyToolCall(tool, input, cwd, roots)
	concerns, needs := as.NeedsAgreement()
	if !needs {
		return HostDecision{Allow: true, Assessment: as}
	}

	var drift, covered []trust.Effect
	if g.Ledger != nil {
		covered, drift = g.Ledger.Cover(concerns)
	} else {
		drift = concerns
	}

	report := HostReport{
		Tool:    tool,
		Zone:    as.Zone(),
		Summary: as.Summary(),
		Effects: concerns,
		Covered: len(drift) == 0 && len(covered) > 0,
	}

	if len(drift) == 0 {
		g.record(report)
		return HostDecision{Allow: true, Assessment: as}
	}

	if g.Policy == HostObserve {
		g.record(report)
		return HostDecision{Allow: true, Assessment: as}
	}

	report.Enforced = true
	g.record(report)

	if g.DeclareTool == "" {
		return HostDecision{Ask: drift, Assessment: as}
	}
	// "Outside what you approved" is judged against whether any approval exists,
	// not against whether this particular call happened to overlap one. A model
	// that declared nginx and then reached for postgresql has drifted even
	// though nothing in that command was covered.
	hadGrants := g.Ledger != nil && len(g.Ledger.List()) > 0
	return HostDecision{Refuse: g.refusal(as, drift, hadGrants), Assessment: as}
}

// refusal is written to the model, so it says what was found, why it stopped,
// and exactly what to do next. A refusal the model cannot act on turns into a
// retry loop.
//
// It is deliberately short. Most gate hits are not the model insisting on a
// host change; they are an incidental `2>/dev/null` or a scratch file in /tmp,
// where taking another route is both trivial and correct. A paragraph of policy
// for that non-event trains the model to treat the gate as a wall of text, and
// it renders in the UI as a failure when nothing failed.
//
// So: name what was found, say that another route is preferred, and mention the
// declaration tool as the second option rather than the first. The model should
// only spend the user's attention when the task genuinely cannot proceed
// without changing this machine.
func (g *HostGate) refusal(as trust.Assessment, drift []trust.Effect, hadGrants bool) string {
	var b strings.Builder
	if as.Unparsed {
		b.WriteString("Klaudia could not read this command line, so it cannot tell whether it changes this machine. ")
	} else {
		b.WriteString("Changes this machine: ")
		b.WriteString(describeAll(drift))
		b.WriteString(". ")
	}
	if hadGrants {
		b.WriteString("That is outside the scope the user approved. ")
	}
	b.WriteString("Take another route if one exists. ")
	b.WriteString(fmt.Sprintf(
		"If it has to be done this way, call %s to describe the whole operation and why.",
		g.DeclareTool))
	return b.String()
}

// ResolveHostPolicy decides the gate's policy for a session.
//
// The default is enforce. The exception is a config that already carries
// allow/deny rules: those users have a working setup built around
// command-level approval, and silently switching them to a different model
// mid-upgrade would be a surprise in the one area where surprises are least
// welcome. They get observe — the classifier runs and `/trust` shows what it
// found, but nothing is refused — plus a one-time notice. `/trust upgrade`
// flips them over when they are ready.
//
// The bool reports whether that notice should be shown.
func ResolveHostPolicy(configured string, hasLegacyRules bool) (HostPolicy, bool) {
	switch HostPolicy(strings.TrimSpace(configured)) {
	case HostEnforce:
		return HostEnforce, false
	case HostObserve:
		return HostObserve, false
	case HostOff:
		return HostOff, false
	}
	if hasLegacyRules {
		return HostObserve, true
	}
	return HostEnforce, false
}

func describeAll(effects []trust.Effect) string {
	seen := map[string]bool{}
	var parts []string
	for _, e := range effects {
		d := e.Describe()
		if !seen[d] {
			seen[d] = true
			parts = append(parts, d)
		}
	}
	return strings.Join(parts, ", ")
}

// approveHostChange puts a host change straight to the user, for frontends with
// no declaration tool wired up. Approval mints a grant so the rest of the
// operation proceeds — otherwise the second command of an approved change would
// stop again, which is the fatigue this design exists to remove.
func (l *Loop) approveHostChange(ctx context.Context, tu anthropic.BetaToolUseBlock, raw []byte, opts Options, hd HostDecision) bool {
	approver := opts.Approver
	if approver == nil {
		approver = DenyAll
	}
	summary := hd.Assessment.Summary()
	hadGrants := opts.Host != nil && opts.Host.Ledger != nil && len(opts.Host.Ledger.List()) > 0

	ad := approver.Approve(ctx, ApprovalRequest{
		ToolName:   tu.Name,
		ToolUseID:  tu.ID,
		Input:      raw,
		Suggestion: summary,
		HostChange: &HostChange{
			Summary: summary,
			Zone:    hd.Assessment.Zone(),
			Effects: hd.Ask,
			Drift:   hadGrants,
		},
	})
	if ad.Behavior != permission.Allow {
		return false
	}
	if opts.Host != nil && opts.Host.Ledger != nil {
		// Best effort: an effect with no scope vocabulary (a reboot, a firewall
		// rule) records nothing and will be asked about again, which is right.
		_, _ = opts.Host.Ledger.MintFromEffects(summary, "approved when it was attempted", hd.Ask)
	}
	return true
}

// hostDeclinedMsg tells the model a host change was refused by the user.
//
// It fails this one tool call, not the turn: work already done stands, and the
// model is told to carry on without the change rather than to stop. A refusal
// that reads as "abort everything" throws away completed work for no reason.
func hostDeclinedMsg(hd HostDecision) string {
	what := describeAll(hd.Ask)
	if what == "" {
		what = "this change to the machine"
	}
	return "The user declined this change to their machine (" + what + "). " +
		"Do not retry it or look for another way to make it. Continue with the rest of the task " +
		"without it, and say what you could not do."
}
