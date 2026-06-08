package tools

import "github.com/greenthread-ai/klaudia/internal/permission"

// editClassDecision is the intrinsic permission decision for file-mutating
// tools (Write, Edit, NotebookEdit): auto-accepted under acceptEdits, blocked
// in read-only plan mode, denied under dontAsk, otherwise ask. (bypass is
// handled upstream by permission.Check.)
func editClassDecision(pctx permission.Context) permission.Decision {
	switch pctx.Mode() {
	case permission.ModeAcceptEdits:
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
	switch pctx.Mode() {
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
// network or drive a real browser with a persistent profile (WebSearch,
// WebFetch, BrowserNavigate, BrowserSnapshot). These are NOT side-effect-free,
// so they are blocked in read-only plan mode, denied under dontAsk, and
// otherwise ask (acceptEdits does not auto-accept — that's only for file
// edits). Users can pre-approve with an allow rule, e.g. /allow WebSearch.
func networkClassDecision(pctx permission.Context) permission.Decision {
	switch pctx.Mode() {
	case permission.ModePlan:
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; web/network access is not allowed"}
	case permission.ModeDontAsk:
		return permission.Decision{Behavior: permission.Deny, Message: "not pre-approved (dontAsk mode)"}
	default:
		return permission.Decision{Behavior: permission.Ask}
	}
}
