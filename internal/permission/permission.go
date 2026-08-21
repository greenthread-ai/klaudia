// Package permission implements Klaudia's permission modes and the allow/deny
// rule evaluation that gates tool execution.
//
// Most of what this package used to decide is now decided by zone, before this
// package is consulted: agent.HostGate classifies each tool call and stops
// changes to the machine Klaudia is running on. What is left here is narrower —
// the user's stance (get on with it / look but do not touch / no checks) and
// the rules a user wrote by hand.
//
// It is a leaf package (no project imports) so both the tools and agent
// packages can depend on it without import cycles. The model mirrors the JS
// checkToolPermissions flow (07-app-features.js): deny rules, then allow rules,
// then mode logic, then the tool's own intrinsic check.
package permission

import "strings"

// Mode is the user's stance for the session.
type Mode string

const (
	// ModeAutonomous lets Klaudia finish the task without per-action prompts.
	//
	// This is the mode the trust model is built for and the default for a fresh
	// config. What used to be a per-command question is now answered by zone:
	// project work runs, and changes to this machine are stopped by the host
	// gate before this package is ever consulted. Selecting it without an
	// enforcing gate would be indistinguishable from bypassPermissions, so the
	// CLI only resolves to it when the gate is enforcing.
	ModeAutonomous Mode = "autonomous"
	// ModeDefault prompts for dangerous operations (interactive only).
	//
	// Superseded by ModeAutonomous. Still honoured: configs written before the
	// trust model say "default", and those users keep asking-per-action until
	// they run /trust upgrade.
	ModeDefault Mode = "default"
	// ModeAcceptEdits auto-accepts file edits; other dangerous ops still ask.
	ModeAcceptEdits Mode = "acceptEdits"
	// ModeBypassPermissions allows everything (--dangerously-skip-permissions).
	ModeBypassPermissions Mode = "bypassPermissions"
	// ModePlan is read-only exploration; mutations are blocked.
	ModePlan Mode = "plan"
	// ModeDontAsk denies anything not pre-approved (non-interactive).
	ModeDontAsk Mode = "dontAsk"
)

// Valid reports whether m is a recognized mode.
func (m Mode) Valid() bool {
	switch m {
	case ModeAutonomous, ModeDefault, ModeAcceptEdits, ModeBypassPermissions, ModePlan, ModeDontAsk:
		return true
	}
	return false
}

// Label is a human-friendly description of the mode for the UI (no raw
// identifiers like "default").
func (m Mode) Label() string {
	switch m {
	case ModeAutonomous:
		return "Autonomous — finish the task; ask before changing this machine"
	case ModeDefault:
		return "Ask before risky operations (superseded by autonomous)"
	case ModeAcceptEdits:
		return "Auto-accept file edits (still ask for other risky ops)"
	case ModeBypassPermissions:
		return "Bypass ALL permission checks (dangerous — tools run without asking)"
	case ModePlan:
		return "Plan mode — read-only, mutations blocked"
	case ModeDontAsk:
		return "Deny anything not pre-approved"
	default:
		return string(m)
	}
}

// SelectableModes are the modes a user may switch to interactively.
//
// Three, not six. The old set asked the user to pick a stance on file edits
// versus commands versus network, which is a question about tool categories —
// the wrong axis. Zones answer it now, so what is left is genuinely different
// intents: get the work done, look but do not touch, and no checks at all.
// The legacy modes stay Valid so existing configs keep working; they are just
// not offered as a choice any more.
func SelectableModes() []Mode {
	return []Mode{ModeAutonomous, ModePlan, ModeBypassPermissions}
}

// Behavior is the outcome of a permission check ("allow" | "deny" | "ask"),
// matching the JS behaviors.
type Behavior string

const (
	Allow Behavior = "allow"
	Deny  Behavior = "deny"
	Ask   Behavior = "ask"
)

// Decision is the result of a permission evaluation. Message explains a deny/ask.
type Decision struct {
	Behavior Behavior
	Message  string
}

// Rule is an allow/deny entry: a tool name with an optional specifier
// (e.g. tool "Bash", specifier "git status:*"). An empty Specifier matches any
// invocation of the tool.
type Rule struct {
	Tool      string
	Specifier string
}

// Context carries the active mode and the allow/deny rule sets for a run.
// Mode is a function rather than a value so each permission check reads the
// live session setting at decision time — when a user types `/mode bypass`
// mid-turn (or mid /goal-iteration), subsequent tool dispatches see the new
// mode without waiting for the next TUI turn boundary. Callers with a fixed
// mode (tests, headless single-shot) wrap their value with StaticMode.
type Context struct {
	Mode  func() Mode
	Allow []Rule
	Deny  []Rule
}

// StaticMode returns a Mode-function that always reports m. Convenience for
// callers without a live mode source — tests, headless one-shot runs, and
// any spot where the mode genuinely cannot change for the lifetime of the
// Context.
func StaticMode(m Mode) func() Mode {
	return func() Mode { return m }
}

// CurrentMode returns c.Mode() with a nil-safe default of ModeDefault. Use
// this instead of calling c.Mode() directly — it keeps tests that construct
// a zero-value Context (e.g. Options{}, no Permission set) compatible with
// the live-mode refactor.
func CurrentMode(c Context) Mode {
	if c.Mode == nil {
		return ModeDefault
	}
	return c.Mode()
}

// IntrinsicChecker is implemented by a tool to express its own permission
// stance given the mode (e.g. Read always allows; Edit allows under acceptEdits
// but asks under default). It is consulted only after rule/mode short-circuits.
type IntrinsicChecker interface {
	// Name is the tool name used for rule matching.
	Name() string
	// CheckPermissions returns the tool's intrinsic decision for this input,
	// considering the current mode.
	CheckPermissions(pctx Context, perm PermissionRequest) Decision
}

// PermissionRequest describes the concrete action a tool wants to take, used
// both for rule matching (Specifier) and for the tool's intrinsic check.
type PermissionRequest struct {
	// Specifier is the rule-matchable description of the action (e.g. a file
	// path for Edit, the command for Bash). May be empty.
	Specifier string
}

// matches reports whether rule r applies to tool name with the given specifier.
// A trailing ":*" or "*" on the rule specifier is treated as a prefix match.
func (r Rule) matches(tool, specifier string) bool {
	if r.Tool != tool {
		return false
	}
	if r.Specifier == "" {
		return true
	}
	pat := r.Specifier
	if strings.HasSuffix(pat, ":*") {
		return strings.HasPrefix(specifier, strings.TrimSuffix(pat, ":*"))
	}
	if strings.HasSuffix(pat, "*") {
		return strings.HasPrefix(specifier, strings.TrimSuffix(pat, "*"))
	}
	return pat == specifier
}

// anyMatch reports whether any rule in rules matches.
func anyMatch(rules []Rule, tool, specifier string) bool {
	for _, r := range rules {
		if r.matches(tool, specifier) {
			return true
		}
	}
	return false
}

// MatchAny reports whether any rule matches the given tool + specifier. Exported
// for frontends (e.g. the TUI's "allow & remember") to check their own rule sets.
func MatchAny(rules []Rule, tool, specifier string) bool {
	return anyMatch(rules, tool, specifier)
}

// Check evaluates the full permission flow for a tool invocation:
//
//  1. deny rules        → deny
//  2. bypassPermissions → allow
//  3. allow rules       → allow
//  4. tool intrinsic    → its decision (may consider acceptEdits/plan)
func Check(pctx Context, tool IntrinsicChecker, req PermissionRequest) Decision {
	name := tool.Name()
	if anyMatch(pctx.Deny, name, req.Specifier) {
		return Decision{Behavior: Deny, Message: "denied by permission rule"}
	}
	if CurrentMode(pctx) == ModeBypassPermissions {
		return Decision{Behavior: Allow}
	}
	if anyMatch(pctx.Allow, name, req.Specifier) {
		return Decision{Behavior: Allow}
	}
	return tool.CheckPermissions(pctx, req)
}
