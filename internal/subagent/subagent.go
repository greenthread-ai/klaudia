// Package subagent defines the built-in sub-agent types and filters a tool
// registry down to the set a given agent type may use. Sub-agents are spawned
// by the Agent tool and run their own agentic loop with a restricted toolset.
package subagent

import (
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Type describes a sub-agent kind selectable via the Agent tool's
// subagent_type, mirroring the JS built-in agents (general-purpose/Explore/Plan).
type Type struct {
	Name         string
	Description  string   // shown to the model in the Agent tool prompt
	SystemPrompt string   // the sub-agent's system prompt
	Tools        []string // tool names the sub-agent may use; ["*"] = all
	// ReadOnlyMCP additionally grants every connected MCP tool that declares
	// itself read-only.
	//
	// The read-only agents are the ones that most want MCP. Fanning out across
	// a wiki, an issue tracker and a chat archive is exactly the work they
	// exist for, and it is also the work whose bulk should never reach the main
	// thread — a sub-agent spends its own context and returns a summary. Naming
	// those tools in Tools is not an option: they are discovered at connect
	// time and differ per project.
	//
	// Read-only is enforced from what the tool declares, not from the agent's
	// system prompt. An instruction not to write is a request; this is the
	// registry the sub-agent is handed.
	ReadOnlyMCP bool
	// Mode, when set, is the permission mode the child runs under regardless of
	// the parent's.
	//
	// It is how a read-only agent can be given Bash. Withholding the tool was
	// the old enforcement, and it made the two research agents unable to run
	// `git log` or `go doc` — the commands research is actually made of, and
	// the ones with no Read/Glob/Grep equivalent. Plan mode now refuses
	// mutation rather than execution (docs/plan-mode.md), so the mode is both a
	// stronger guarantee than a short tool list and a less crippling one.
	//
	// Deliberately not applied over ModeBypassPermissions: that flag means the
	// user has taken responsibility for the session, and a child gate they
	// could not switch off would be the one exception to it.
	Mode permission.Mode
}

// readOnlyTool is implemented by tools that can state they only read. MCP tools
// do, from the protocol's readOnlyHint annotation or from .mcp.json.
type readOnlyTool interface{ ReadOnly() bool }

// toolSearchName is the tool that loads deferred tools on demand. Named here
// rather than imported to keep this package free of a dependency on the tool
// implementations it filters.
const toolSearchName = "ToolSearch"

// Builtin returns the built-in sub-agent types.
func Builtin() []Type {
	return []Type{
		{
			Name: "general-purpose",
			Description: "General-purpose agent for researching complex questions, searching for code, " +
				"and executing multi-step tasks. Use when you need to search and are not confident you'll " +
				"find the right match in the first few tries.",
			Tools: []string{"*"},
			SystemPrompt: "You are an agent for Klaudia. Given the user's message, use the available tools to " +
				"complete the task. Do what has been asked; nothing more, nothing less. When you complete the " +
				"task, respond with a detailed writeup of what you found or did.",
		},
		{
			Name: "Explore",
			Description: "Read-only search agent for broad fan-out searches across many files and " +
				"read-only MCP sources. It locates code and answers questions but does not modify anything.",
			Tools:       []string{"Read", "Glob", "Grep", "Bash"},
			ReadOnlyMCP: true,
			Mode:        permission.ModePlan,
			SystemPrompt: "You are a read-only exploration agent for Klaudia. You may read, search and run " +
				"commands that only inspect — anything that would change a file is refused. Thoroughly " +
				"investigate and return a concise, well-organized summary of your findings with file paths " +
				"and line references.",
		},
		{
			Name: "Plan",
			Description: "Read-only architect agent that designs an implementation plan and returns it. " +
				"Use to plan a change before implementing.",
			Tools:       []string{"Read", "Glob", "Grep", "Bash"},
			ReadOnlyMCP: true,
			Mode:        permission.ModePlan,
			SystemPrompt: "You are a software architect agent for Klaudia. Explore the codebase and produce a " +
				"concrete, step-by-step implementation plan. You may run commands that only inspect — " +
				"`git log`, `git diff`, `ls`, `sed -n` — but anything that would change a file is " +
				"refused, so note explicitly anything you could not verify. Identify the critical files and " +
				"trade-offs. Return the plan as your final message.",
		},
	}
}

// Lookup returns the built-in type with the given name.
func Lookup(name string) (Type, bool) {
	for _, t := range Builtin() {
		if t.Name == name {
			return t, true
		}
	}
	return Type{}, false
}

// Filter returns a new registry containing only the tools this type may use,
// drawn from base. A Tools entry of "*" includes everything in base.
func (t Type) Filter(base *tools.Registry) *tools.Registry {
	if len(t.Tools) == 1 && t.Tools[0] == "*" {
		var all []tools.Tool
		for _, name := range base.Names() {
			if tool, ok := base.Lookup(name); ok {
				all = append(all, tool)
			}
		}
		return tools.NewRegistry(all...)
	}
	var allowed []tools.Tool
	seen := make(map[string]bool, len(t.Tools))
	for _, name := range t.Tools {
		if tool, ok := base.Lookup(name); ok {
			allowed = append(allowed, tool)
			seen[name] = true
		}
	}
	if t.ReadOnlyMCP {
		var granted int
		for _, name := range base.Names() {
			if seen[name] {
				continue
			}
			tool, ok := base.Lookup(name)
			if !ok {
				continue
			}
			if ro, isRO := tool.(readOnlyTool); isRO && ro.ReadOnly() {
				allowed = append(allowed, tool)
				granted++
			}
		}
		// MCP tools are numerous, so the parent withholds them from the request
		// behind ToolSearch and loads them on demand. A sub-agent inherits that
		// arrangement, so granting the tools without granting the means to find
		// them grants nothing: the sub-agent holds tools it can neither see nor
		// load, and reports — accurately — that it has only Read, Glob and Grep.
		//
		// Registry membership was what the first version asserted, and
		// membership was never the thing that mattered.
		if granted > 0 && !seen[toolSearchName] {
			if ts, ok := base.Lookup(toolSearchName); ok {
				allowed = append(allowed, ts)
			}
		}
	}
	return tools.NewRegistry(allowed...)
}
