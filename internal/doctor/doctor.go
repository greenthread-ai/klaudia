// Package doctor produces a diagnostic report of Klaudia's environment: which
// sandbox backends are available, whether authentication is configured, and the
// resolved provider/model/MCP setup. It backs the /doctor command.
package doctor

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/version"
)

// Status values for a Check.
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusInfo = "info"
)

// Check is one diagnostic line.
type Check struct {
	Name   string
	Status string // StatusOK | StatusWarn | StatusInfo
	Detail string
}

// Input carries facts the CLI already resolved, so doctor stays pure and
// testable. doctor adds OS/binary detection itself.
type Input struct {
	Provider    string // resolved provider ("anthropic" | "openai" | …)
	Model       string // resolved model id
	SandboxMode string // configured sandbox mode ("local" | "os" | "container")
	ConfigFound bool   // a .klaudia/config.toml was loaded
	AuthOK      bool   // a usable credential resolved
	AuthKind    string // "oauth" | "api-key" | "none"
	MCPServers  int    // configured MCP server count
	// MCPLegacySSE names the configured servers still on the HTTP+SSE
	// transport, deprecated by the MCP spec in favour of streamable HTTP.
	MCPLegacySSE []string
	LSPServers   []LSPServer // detected language servers
	Skills       []Skill     // user-defined skills loaded for this session
	Hooks        []Hook      // configured lifecycle hooks
	// Context-window facts resolved by the CLI (api.ContextWindow). Zero limit
	// means the model isn't in our table and no config override was set — we
	// fall back to compaction's default at request time.
	ContextWindow int
	ContextSource string
	// Effort is the resolved level ("" when unset, i.e. the provider's own
	// default), and MaxOutputTokens the per-response cap it has to fit in.
	// Both are reported together because the cap bounds thinking and text
	// combined, so a high level against a small cap truncates.
	Effort          string
	MaxOutputTokens int
	// MissingLSPHints are actionable suggestions for languages present in the
	// project but lacking a server (e.g. "install gopls for go support").
	MissingLSPHints []string
	// SyncedIgnored counts skills found under a backend-synced directory and
	// deliberately not loaded, so they stay discoverable without being
	// injected into context.
	SyncedIgnored int
}

// effortMinOutputTokens is the output cap below which a high effort level is
// a truncation risk rather than a setting. Anthropic's own guidance for xhigh
// and max starts at 64k; that is the number used here rather than a guess.
const effortMinOutputTokens = 64000

// effortNeedsRoom reports whether a level is one of the two the docs call out
// as needing a large max_tokens.
func effortNeedsRoom(level string) bool {
	return level == "xhigh" || level == "max"
}

// Skill is one loaded user-defined skill, for the /doctor report.
type Skill struct {
	Name  string // invocation name
	Scope string // "project" | "user"
}

// LSPServer is a detected language server for the /doctor report.
type LSPServer struct {
	Name     string // binary, e.g. "gopls"
	Language string // e.g. "go"
	Version  string // best-effort, e.g. "v0.15.2"; "" if unknown
}

// Hook is one configured lifecycle hook, for the /doctor report.
//
// Flattened to strings here rather than importing internal/hooks, for the same
// reason Skill is: doctor describes an environment and must not acquire a
// dependency on the machinery it describes.
type Hook struct {
	Event   string // "PreToolUse", …
	Matcher string // tool-name pattern; "" means every tool
	Scope   string // "user" | "project"
	// Dormant marks a hook that is configured but will not run: a project hook
	// whose set has not been approved on this machine. Reported because the
	// symptom is silence — the user wrote a formatter hook, nothing formats, and
	// nothing in the transcript says the set is still waiting on a yes.
	Dormant bool
}

// lookPath is indirected for testing.
var lookPath = exec.LookPath

// Run builds the diagnostic report.
func Run(in Input) []Check {
	var checks []Check
	add := func(name, status, detail string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail})
	}

	add("platform", StatusInfo, runtime.GOOS+"/"+runtime.GOARCH)

	// Which binary is this? The version constant cannot say — it mirrors a
	// retired JS bundle and never moves — so report the commit the toolchain
	// embedded. "Is the fix in?" has been answered by comparing file mtimes to
	// git log more than once.
	if b := version.Build(); b != "" {
		add("build", StatusInfo, version.Version+" "+b)
	} else {
		add("build", StatusInfo, version.Version+" (no build metadata embedded)")
	}

	// Skills are invisible when none are defined: with zero skills the Skill
	// tool is not registered at all, so asking the model whether skills work
	// gets "I have no such ability" — indistinguishable from a broken feature.
	// This is the only place that can tell the two apart.
	syncedNote := ""
	if in.SyncedIgnored > 0 {
		syncedNote = fmt.Sprintf("; %d synced skill(s) ignored (backend-managed — copy one into .klaudia/skills to use it)", in.SyncedIgnored)
	}
	if len(in.Skills) == 0 {
		add("skills", StatusInfo, "none loaded (add .md files to .klaudia/skills or ~/.klaudia/skills)"+syncedNote)
	} else {
		names := make([]string, 0, len(in.Skills))
		for _, sk := range in.Skills {
			names = append(names, sk.Name+" ("+sk.Scope+")")
		}
		sort.Strings(names)
		add("skills", StatusOK, strconv.Itoa(len(in.Skills))+" loaded: "+strings.Join(names, ", ")+syncedNote)
	}

	// Authentication.
	switch {
	case in.AuthOK && in.AuthKind != "":
		add("auth", StatusOK, "credential resolved ("+in.AuthKind+")")
	case in.AuthOK:
		add("auth", StatusOK, "credential resolved")
	default:
		add("auth", StatusWarn, "no credential resolved (set an API key or run an OAuth login)")
	}

	// Provider / model.
	provider := in.Provider
	if provider == "" {
		provider = "anthropic"
	}
	model := in.Model
	if model == "" {
		model = "(default)"
	}
	add("provider", StatusInfo, provider+" — model "+model)

	// Context window — show the limit klaudia will request against, plus where
	// it came from, so users know whether their `contextWindow` config override
	// is taking effect.
	if in.ContextWindow > 0 {
		add("context", StatusInfo, fmt.Sprintf("%s tokens (%s)", formatTokens(in.ContextWindow), in.ContextSource))
	} else if in.ContextSource != "" {
		add("context", StatusWarn, in.ContextSource)
	}

	// Effort, and the one way it goes wrong quietly. max_tokens is a hard
	// limit on thinking plus response text, so a high level against the 8192
	// floor an unknown model gets does not fail — it truncates mid-thought,
	// which reads as the model losing the plot rather than as a setting.
	if in.Effort != "" {
		msg := "effort " + in.Effort
		if in.MaxOutputTokens > 0 {
			msg += fmt.Sprintf(" · %s max output", formatTokens(in.MaxOutputTokens))
		}
		if effortNeedsRoom(in.Effort) && in.MaxOutputTokens > 0 && in.MaxOutputTokens < effortMinOutputTokens {
			add("effort", StatusWarn, msg+fmt.Sprintf(" — %s bounds thinking and text together; raise maxTokens (at least %s) or lower the effort",
				formatTokens(in.MaxOutputTokens), formatTokens(effortMinOutputTokens)))
		} else {
			add("effort", StatusInfo, msg)
		}
	}

	// Config presence.
	if in.ConfigFound {
		add("config", StatusOK, ".klaudia/config.toml loaded")
	} else {
		add("config", StatusInfo, "no .klaudia/config.toml (using defaults)")
	}

	// Sandbox: report on the configured mode's required backend.
	checks = append(checks, sandboxCheck(in.SandboxMode))

	// Hooks run shell commands around the model's work, so the report says what
	// is attached where even when everything is healthy.
	checks = append(checks, hooksCheck(in.Hooks))

	// Container runtimes (informational, useful regardless of mode).
	for _, rt := range []string{"docker", "podman"} {
		if _, err := lookPath(rt); err == nil {
			add("runtime:"+rt, StatusOK, "available")
		} else {
			add("runtime:"+rt, StatusInfo, "not found")
		}
	}

	// MCP.
	if in.MCPServers > 0 {
		add("mcp", StatusOK, fmt.Sprintf("%d server(s) configured", in.MCPServers))
	} else {
		add("mcp", StatusInfo, "no MCP servers configured")
	}
	// The legacy HTTP+SSE transport still works and is still supported here,
	// but it is deprecated in the spec and servers drop it on their own
	// schedule. The symptom when one does is a connect error with no hint that
	// the fix is one word in .mcp.json.
	if len(in.MCPLegacySSE) > 0 {
		add("mcp:transport", StatusWarn, fmt.Sprintf(
			"%s on the deprecated HTTP+SSE transport (drop `\"type\": \"sse\"` to use streamable HTTP)",
			strings.Join(in.MCPLegacySSE, ", ")))
	}

	// LSP code-intel servers (detected, not downloaded): one line per language,
	// then a summary — actionable warning when a project language lacks a server.
	for _, s := range in.LSPServers {
		detail := s.Name
		if s.Version != "" {
			detail += " (" + s.Version + ")"
		}
		add("lsp:"+s.Language, StatusOK, detail)
	}
	switch {
	case len(in.MissingLSPHints) > 0:
		add("lsp", StatusWarn, strings.Join(in.MissingLSPHints, "; "))
	case len(in.LSPServers) > 0:
		add("lsp", StatusOK, fmt.Sprintf("%d language server(s) detected", len(in.LSPServers)))
	default:
		add("lsp", StatusInfo, "no language servers detected (install gopls, rust-analyzer, …)")
	}

	return checks
}

// formatTokens renders a token count compactly: "200K", "1M", or the raw
// integer when smaller than 1K (uncommon for context windows but possible if a
// user pins a tiny override). Doesn't aim for SI precision — just a readable
// label in /doctor and /stats lines.
func formatTokens(n int) string {
	switch {
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000 && n%1_000 == 0:
		return fmt.Sprintf("%dK", n/1_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// hooksCheck summarises the configured lifecycle hooks.
//
// Listed in configuration order and not sorted, unlike skills: hooks run in the
// order they are declared, and a formatter before a linter is not the same
// automation as the reverse, so the report shows the order that is in force.
func hooksCheck(hs []Hook) Check {
	if len(hs) == 0 {
		return Check{"hooks", StatusInfo, "none configured (add [[hooks]] to .klaudia/config.toml)"}
	}
	var user, project, dormant int
	labels := make([]string, 0, len(hs))
	for _, h := range hs {
		label := h.Event
		if h.Matcher != "" {
			label += "(" + h.Matcher + ")"
		}
		if h.Scope == "project" {
			project++
		} else {
			user++
		}
		if h.Dormant {
			dormant++
		}
		labels = append(labels, label)
	}
	scopes := make([]string, 0, 2)
	if user > 0 {
		scopes = append(scopes, fmt.Sprintf("%d user", user))
	}
	if project > 0 {
		scopes = append(scopes, fmt.Sprintf("%d project", project))
	}
	detail := strings.Join(scopes, ", ") + ": " + strings.Join(labels, ", ")
	if dormant > 0 {
		return Check{"hooks", StatusWarn, detail + fmt.Sprintf(
			" — %d from the project will not run until you confirm the set", dormant)}
	}
	return Check{"hooks", StatusOK, detail}
}

// sandboxCheck reports whether the backend required by the configured sandbox
// mode is present.
func sandboxCheck(mode string) Check {
	switch mode {
	case "os":
		bin, label := osSandboxBinary()
		if bin == "" {
			return Check{"sandbox", StatusInfo, "mode \"os\" unsupported on " + runtime.GOOS}
		}
		if _, err := lookPath(bin); err == nil {
			return Check{"sandbox", StatusOK, "mode \"os\" via " + label}
		}
		return Check{"sandbox", StatusWarn, "mode \"os\" needs " + bin + " (not found); falls back to local"}
	case "container":
		return Check{"sandbox", StatusInfo, "mode \"container\" (see runtime checks below)"}
	default:
		return Check{"sandbox", StatusInfo, "mode \"local\" (unconfined host execution)"}
	}
}

// osSandboxBinary returns the confinement binary and label for the current OS.
func osSandboxBinary() (bin, label string) {
	switch runtime.GOOS {
	case "darwin":
		return "sandbox-exec", "sandbox-exec (Seatbelt)"
	case "linux":
		return "bwrap", "bubblewrap"
	default:
		return "", ""
	}
}

// Format renders the report as aligned lines for display.
func Format(checks []Check) string {
	icon := map[string]string{StatusOK: "✓", StatusWarn: "!", StatusInfo: "·"}
	out := "Klaudia doctor:"
	for _, c := range checks {
		mark := icon[c.Status]
		if mark == "" {
			mark = "·"
		}
		out += fmt.Sprintf("\n  %s %-16s %s", mark, c.Name, c.Detail)
	}
	return out
}
