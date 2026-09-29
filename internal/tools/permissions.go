package tools

import (
	"path/filepath"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// editClassDecision is the intrinsic permission decision for file-mutating
// tools (Write, Edit, NotebookEdit): auto-accepted under acceptEdits, blocked
// in read-only plan mode, denied under dontAsk, otherwise ask. (bypass is
// handled upstream by permission.Check.)
func editClassDecision(pctx permission.Context) permission.Decision {
	switch permission.CurrentMode(pctx) {
	case permission.ModeAutonomous, permission.ModeAcceptEdits:
		return permission.Decision{Behavior: permission.Allow}
	case permission.ModePlan:
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; file modifications are not allowed"}
	case permission.ModeDontAsk:
		return permission.Decision{Behavior: permission.Deny, Message: "not pre-approved (dontAsk mode)"}
	default:
		return permission.Decision{Behavior: permission.Ask}
	}
}

// execClassDecision is the intrinsic decision for command-executing tools
// (Bash): acceptEdits does NOT auto-accept execution, plan blocks it, dontAsk
// denies, otherwise ask.
func execClassDecision(pctx permission.Context) permission.Decision {
	switch permission.CurrentMode(pctx) {
	case permission.ModeAutonomous:
		// Running commands is the job. What a command may reach is decided by
		// the host gate before this is consulted, not by asking here.
		return permission.Decision{Behavior: permission.Allow}
	case permission.ModePlan:
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; command execution is not allowed"}
	case permission.ModeDontAsk:
		return permission.Decision{Behavior: permission.Deny, Message: "not pre-approved (dontAsk mode)"}
	default:
		return permission.Decision{Behavior: permission.Ask}
	}
}

// allowAlways is the intrinsic decision for read-only / side-effect-free tools
// (Read, Glob, Grep, TodoWrite).
func allowAlways(permission.Context) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}

// networkClassDecision is the intrinsic decision for tools that reach the
// network or drive a real browser with a persistent profile (BrowserSearch,
// BrowserFetch, BrowserNavigate, BrowserSnapshot). These are NOT side-effect-free,
// so they are blocked in read-only plan mode, denied under dontAsk, and
// otherwise ask (acceptEdits does not auto-accept — that's only for file
// edits). Users can pre-approve with an allow rule, e.g. /allow BrowserSearch.
func networkClassDecision(pctx permission.Context) permission.Decision {
	switch permission.CurrentMode(pctx) {
	case permission.ModeAutonomous:
		// Fetching things is ordinary work and changes nothing on this machine.
		return permission.Decision{Behavior: permission.Allow}
	case permission.ModePlan:
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; web/network access is not allowed"}
	case permission.ModeDontAsk:
		return permission.Decision{Behavior: permission.Deny, Message: "not pre-approved (dontAsk mode)"}
	default:
		return permission.Decision{Behavior: permission.Ask}
	}
}

// pathRequest is the permission request for a tool acting on path. The
// specifier stays the path as written (it names the request in prompts and
// "always allow" rules); the forms rules are checked against add the absolute
// path and, when a symlink is involved, the path it resolves to — so a rule
// for ~/.ssh/** also covers ./link-to-ssh/id_rsa.
func pathRequest(path string) permission.PermissionRequest {
	if path == "" {
		return permission.PermissionRequest{}
	}
	forms := []string{path}
	add := func(f string) {
		for _, have := range forms {
			if have == f {
				return
			}
		}
		forms = append(forms, f)
	}
	if abs, err := filepath.Abs(path); err == nil {
		add(abs)
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			add(real)
		} else if dir, err := filepath.EvalSymlinks(filepath.Dir(abs)); err == nil {
			add(filepath.Join(dir, filepath.Base(abs))) // a new file in a linked directory
		}
	}
	return permission.PermissionRequest{Specifier: path, Commands: [][]string{forms}}
}

func firstNonEmptyPath(p, fallback string) string {
	if p == "" {
		return fallback
	}
	return p
}

// globBase is the directory at the front of a glob pattern, before the first
// component with a glob character in it.
func globBase(pattern string) string {
	parts := strings.Split(filepath.ToSlash(pattern), "/")
	for i, part := range parts {
		if strings.ContainsAny(part, "*?[{") {
			if i == 0 {
				return "."
			}
			return filepath.FromSlash(strings.Join(parts[:i], "/") + "/")
		}
	}
	return pattern
}
