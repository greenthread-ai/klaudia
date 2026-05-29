// Package doctor produces a diagnostic report of Klaudia's environment: which
// sandbox backends are available, whether authentication is configured, and the
// resolved provider/model/MCP setup. It backs the /doctor command.
package doctor

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
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
	Provider    string      // resolved provider ("anthropic" | "openai" | …)
	Model       string      // resolved model id
	SandboxMode string      // configured sandbox mode ("local" | "os" | "container")
	ConfigFound bool        // a .klaudia/config.toml was loaded
	AuthOK      bool        // a usable credential resolved
	AuthKind    string      // "oauth" | "api-key" | "none"
	MCPServers  int         // configured MCP server count
	LSPServers  []LSPServer // detected language servers
	// Context-window facts resolved by the CLI (api.ContextWindow). Zero limit
	// means the model isn't in our table and no config override was set — we
	// fall back to compaction's default at request time.
	ContextWindow int
	ContextSource string
	// MissingLSPHints are actionable suggestions for languages present in the
	// project but lacking a server (e.g. "install gopls for go support").
	MissingLSPHints []string
}

// LSPServer is a detected language server for the /doctor report.
type LSPServer struct {
	Name     string // binary, e.g. "gopls"
	Language string // e.g. "go"
	Version  string // best-effort, e.g. "v0.15.2"; "" if unknown
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

	// Config presence.
	if in.ConfigFound {
		add("config", StatusOK, ".klaudia/config.toml loaded")
	} else {
		add("config", StatusInfo, "no .klaudia/config.toml (using defaults)")
	}

	// Sandbox: report on the configured mode's required backend.
	checks = append(checks, sandboxCheck(in.SandboxMode))

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
