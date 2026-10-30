package tools

import (
	"github.com/greenthread-ai/klaudia/internal/trust"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// The intrinsic decisions below are what is left of per-tool permission once
// zones took over. Only plan mode has anything to say: it is read-only
// exploration, so the three categories of side effect are each refused with a
// message naming what was blocked. Everything else runs.
//
// bypassPermissions never reaches here — permission.Check short-circuits it.
// A change to this machine never reaches here either; agent.dispatch runs the
// host gate first, and it does not consult the mode.

// editClassDecision is the intrinsic decision for file-mutating tools
// (Write, Edit, NotebookEdit).
func editClassDecision(pctx permission.Context) permission.Decision {
	if permission.CurrentMode(pctx) == permission.ModePlan {
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; file modifications are not allowed"}
	}
	return permission.Decision{Behavior: permission.Allow}
}

// execClassDecision is the intrinsic decision for command-executing tools
// (Bash). What a command may reach on this machine is decided by the host gate
// before this is consulted; what plan mode uniquely protects is the user's
// files, so the axis here is mutation rather than execution.
//
// Refusing every command was the original rule, and it made plan mode unable
// to do the one thing it exists for. Read, Glob and Grep cover files, but
// `git log`, `git diff`, `go doc` and `go list -m` have no tool equivalent at
// all — measured, a planning session refused all four shipped a plan carrying
// the caveat that it had not looked at what was on the branch it was planning
// against. See docs/plan-mode.md.
func execClassDecision(pctx permission.Context, req permission.PermissionRequest) permission.Decision {
	if permission.CurrentMode(pctx) != permission.ModePlan {
		return permission.Decision{Behavior: permission.Allow}
	}
	cmd := req.Raw
	if cmd == "" {
		cmd = req.Specifier
	}
	m, reason, part := trust.InspectCommandPart(cmd)
	switch m {
	case trust.MutationNone:
		return permission.Decision{Behavior: permission.Allow}
	case trust.MutationPresent:
		return permission.Decision{
			Behavior: permission.Deny,
			Message:  "plan mode does not change files, and " + reason + ". Research it instead, or ask the user to leave plan mode.",
			Subject:  part,
		}
	default:
		// Unclear asks rather than refuses. The classifier is an allowlist and
		// allowlists go stale; one that fails to a question costs a keypress,
		// one that fails to a denial recreates the bug above for every program
		// nobody has got round to classifying yet.
		return permission.Decision{
			Behavior: permission.Ask,
			Message:  "Plan mode is read-only and " + reason + ".",
			Subject:  part,
		}
	}
}

// allowAlways is the intrinsic decision for read-only / side-effect-free tools
// (Read, Glob, Grep, TodoWrite).
func allowAlways(permission.Context) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}

// networkClassDecision is the intrinsic decision for tools that reach the
// network or drive a real browser with a persistent profile (BrowserSearch,
// BrowserFetch, BrowserNavigate, BrowserSnapshot). Fetching things is ordinary
// work and changes nothing on this machine, but it is not side-effect-free, so
// read-only plan mode still refuses it.
func networkClassDecision(pctx permission.Context) permission.Decision {
	if permission.CurrentMode(pctx) == permission.ModePlan {
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; web/network access is not allowed"}
	}
	return permission.Decision{Behavior: permission.Allow}
}
