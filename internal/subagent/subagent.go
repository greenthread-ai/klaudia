// Package subagent defines the built-in sub-agent types and filters a tool
// registry down to the set a given agent type may use. Sub-agents are spawned
// by the Agent tool and run their own agentic loop with a restricted toolset.
package subagent

import "github.com/greenthread/klaudia/internal/tools"

// Type describes a sub-agent kind selectable via the Agent tool's
// subagent_type, mirroring the JS built-in agents (general-purpose/Explore/Plan).
type Type struct {
	Name         string
	Description  string   // shown to the model in the Agent tool prompt
	SystemPrompt string   // the sub-agent's system prompt
	Tools        []string // tool names the sub-agent may use; ["*"] = all
}

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
			Description: "Read-only search agent for broad fan-out searches across many files. " +
				"It locates code and answers questions but does not modify anything.",
			Tools: []string{"Read", "Glob", "Grep"},
			SystemPrompt: "You are a read-only exploration agent for Klaudia. You may ONLY read and search; " +
				"never modify files or run mutating commands. Thoroughly investigate and return a concise, " +
				"well-organized summary of your findings with file paths and line references.",
		},
		{
			Name: "Plan",
			Description: "Read-only architect agent that designs an implementation plan and returns it. " +
				"Use to plan a change before implementing.",
			Tools: []string{"Read", "Glob", "Grep"},
			SystemPrompt: "You are a software architect agent for Klaudia. Explore the codebase (read-only) and " +
				"produce a concrete, step-by-step implementation plan. Identify the critical files and trade-offs. " +
				"Return the plan as your final message; do not modify anything.",
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
	for _, name := range t.Tools {
		if tool, ok := base.Lookup(name); ok {
			allowed = append(allowed, tool)
		}
	}
	return tools.NewRegistry(allowed...)
}
