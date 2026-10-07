package tools

import (
	"path/filepath"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
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
// (Bash). Running commands is the job; what a command may reach is decided by
// the host gate before this is consulted.
func execClassDecision(pctx permission.Context) permission.Decision {
	if permission.CurrentMode(pctx) == permission.ModePlan {
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; command execution is not allowed"}
	}
	return permission.Decision{Behavior: permission.Allow}
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

// execGrantingDirs and execGrantingFiles are project paths whose contents run
// later without anyone running them on purpose: git hooks and config (hooks,
// fsmonitor, filter drivers), agent and editor config that launches servers or
// tasks, and shell/package-manager rc files that execute on cd or install.
// Writing one is how an edit becomes code execution, so they are asked about
// even where edits are otherwise auto-accepted.
var (
	execGrantingDirs  = map[string]bool{".git": true, ".husky": true, ".klaudia": true, ".claude": true, ".devcontainer": true, ".vscode": true}
	execGrantingFiles = map[string]bool{".mcp.json": true, ".envrc": true, ".npmrc": true, ".yarnrc": true, ".yarnrc.yml": true, ".pre-commit-config.yaml": true, "bunfig.toml": true, ".bazelrc": true}
)

// execGranting reports whether writing path would write one of those files. A
// symlinked parent is resolved, so "hooks -> .git/hooks" is caught too.
func execGranting(path string) bool {
	if path == "" {
		return false
	}
	candidates := []string{filepath.Clean(path)}
	if real, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		candidates = append(candidates, filepath.Join(real, filepath.Base(path)))
	}
	for _, p := range candidates {
		if execGrantingFiles[filepath.Base(p)] {
			return true
		}
		for _, part := range strings.Split(filepath.ToSlash(p), "/") {
			if execGrantingDirs[part] {
				return true
			}
		}
	}
	return false
}

// editPathDecision is editClassDecision for a write to path: where edits are
// auto-accepted, an exec-granting path is asked about instead.
func editPathDecision(pctx permission.Context, path string) permission.Decision {
	d := editClassDecision(pctx)
	if d.Behavior == permission.Allow && execGranting(path) {
		return permission.Decision{Behavior: permission.Ask, Message: path + " can make code run later (hooks, tasks, server or shell config), so it is not auto-approved"}
	}
	return d
}
