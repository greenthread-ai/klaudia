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

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

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
	// Trusting reports whether the zone-based trust model is enforcing for this
	// session. It is a function for the same reason Mode is: /trust upgrade
	// should take effect on the next tool call, not the next turn.
	//
	// It is a bool rather than the trust posture itself so that this package
	// stays a leaf. permission is imported by both tools and agent, and giving
	// it a dependency on trust — which needs a filesystem view and a
	// session-scoped ledger — would invert that. The one bit of information
	// that crosses is "is something else already vouching for this call".
	Trusting func() bool
}

// IsTrusting reports c.Trusting() with a nil-safe default of false, so a
// zero-value Context behaves as it did before trust existed.
func IsTrusting(c Context) bool {
	if c.Trusting == nil {
		return false
	}
	return c.Trusting()
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

	// Commands, when set, lists each separate command a request runs — for
	// Bash, every command in the line, including those in $(…), subshells and
	// `bash -c` payloads — each with the forms a rule may name it by (as
	// written, with wrappers such as sudo stripped, and the "program
	// subcommand" short form). Rules are then checked per command rather than
	// against Specifier: a deny rule applies when it matches any form of any
	// command, and allow rules apply only when every command is matched.
	//
	// Checking only the first command — which Specifier alone does — let
	// `Bash(git status:*)` approve `git status && curl … | sh`, and let
	// `ls && rm -rf x` past a `Bash(rm:*)` deny.
	Commands [][]string

	// Opaque marks a request whose commands could not all be read: a parse
	// error, a program name that is an expansion, or a line too long to show
	// in full. Allow rules never apply to it; deny rules still check
	// Specifier and whatever Commands were read.
	Opaque bool
}

// DeniedBy reports whether a deny rule in rules applies to req for tool.
//
// A deny rule for Read also covers Glob and Grep, and one for Edit covers
// Write and NotebookEdit: `Read(~/.ssh/**)` means the files are not to be
// read, not that one tool of three may not read them. Allow rules are not
// widened this way.
func DeniedBy(rules []Rule, tool string, req PermissionRequest) bool {
	names := []string{tool}
	if alias, ok := denyAlias[tool]; ok {
		names = append(names, alias)
	}
	for _, name := range names {
		if anyMatch(rules, name, req.Specifier) {
			return true
		}
		for _, forms := range req.Commands {
			for _, f := range forms {
				if anyMatch(rules, name, f) {
					return true
				}
			}
		}
	}
	return false
}

// denyAlias maps a tool to the tool whose deny rules also apply to it.
var denyAlias = map[string]string{"Glob": "Read", "Grep": "Read", "Write": "Edit", "NotebookEdit": "Edit"}

// pathTools take a file path as their specifier; their rules are path
// patterns.
var pathTools = map[string]bool{"Read": true, "Glob": true, "Grep": true, "Edit": true, "Write": true, "NotebookEdit": true}

// AllowedBy reports whether allow rules in rules cover req for tool. With
// Commands set, every command must be matched by some rule; an Opaque request
// is never covered.
func AllowedBy(rules []Rule, tool string, req PermissionRequest) bool {
	if req.Opaque {
		return false
	}
	if len(req.Commands) == 0 {
		return anyMatch(rules, tool, req.Specifier)
	}
	for _, forms := range req.Commands {
		covered := false
		for _, f := range forms {
			if anyMatch(rules, tool, f) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
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
	if pathTools[r.Tool] && pathMatch(pat, specifier) {
		return true
	}
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
//  1. deny rules        → deny (any command, for a multi-command request)
//  2. bypassPermissions → allow
//  3. allow rules       → allow (every command, for a multi-command request)
//  4. tool intrinsic    → its decision (may consider acceptEdits/plan)
func Check(pctx Context, tool IntrinsicChecker, req PermissionRequest) Decision {
	name := tool.Name()
	if DeniedBy(pctx.Deny, name, req) {
		return Decision{Behavior: Deny, Message: "denied by permission rule"}
	}
	if CurrentMode(pctx) == ModeBypassPermissions {
		return Decision{Behavior: Allow}
	}
	if AllowedBy(pctx.Allow, name, req) {
		return Decision{Behavior: Allow}
	}
	return tool.CheckPermissions(pctx, req)
}

// pathMatch reports whether a file rule's pattern covers an absolute path.
//
// File rules used to be compared as strings: exact, or a prefix before a
// trailing "*". Against the path as the model happened to write it, that made
// `Edit(src/**)` a prefix "src/*" that never matched, left `~` unexpanded, and
// let "./secrets/x" and "/abs/secrets/x" disagree. Patterns now work the way
// they read:
//   - "~" and "~/…" are the home directory;
//   - a relative pattern is relative to the working directory;
//   - "*", "?", "[…]", "{…}" and "**" are globs (doublestar), "**" crossing
//     directories; a trailing "/" means everything beneath;
//   - a pattern with no glob covers that path and everything beneath it.
//
// The old string comparison still runs after this, so existing rules keep
// matching what they matched.
func pathMatch(pat, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	p := pat
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		p = home + p[1:]
	}
	if !filepath.IsAbs(p) {
		cwd, err := os.Getwd()
		if err != nil {
			return false
		}
		p = filepath.Join(cwd, p)
	}
	beneath := strings.HasSuffix(pat, "/")
	p = filepath.Clean(p)
	if !strings.ContainsAny(p, "*?[{") {
		return path == p || strings.HasPrefix(path, p+string(filepath.Separator))
	}
	if ok, _ := doublestar.Match(filepath.ToSlash(p), filepath.ToSlash(path)); ok {
		return true
	}
	if beneath {
		ok, _ := doublestar.Match(filepath.ToSlash(p)+"/**", filepath.ToSlash(path))
		return ok
	}
	return false
}
