package agent

import (
	"fmt"
	"time"

	"github.com/greenthread-ai/klaudia/internal/prompt"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// subagentSystem is a sub-agent's system prompt: its type's prompt followed by
// the environment and project context (see prompt.Subagent). A function here
// rather than a call in Spawn, whose prompt parameter shadows the package.
func subagentSystem(typePrompt, workingDir string) string {
	return prompt.Subagent(typePrompt, workingDir)
}

// subagentTools adjusts a sub-agent's filtered registry for a child that runs
// unattended inside its parent's session.
//
// AskUserQuestion is removed: the Agent tool tells the parent a sub-agent
// "cannot ask follow-up questions", and a child that is offered the tool
// anyway spends turns asking a user who is not there. TodoWrite is rebuilt on a
// store of its own: the "*" filter handed a general-purpose child the parent's
// tool, and with it the parent's todo list, which the child's first TodoWrite
// replaced wholesale.
func subagentTools(reg *tools.Registry) *tools.Registry {
	var out []tools.Tool
	for _, name := range reg.Names() {
		t, ok := reg.Lookup(name)
		if !ok {
			continue
		}
		switch name {
		case "AskUserQuestion":
			continue
		case "TodoWrite":
			own, err := tools.NewTodoWrite(&tools.TodoStore{})
			if err != nil {
				continue // the schema is static; this does not fail in practice
			}
			t = own
		}
		out = append(out, t)
	}
	return tools.NewRegistry(out...)
}

// subagentUsage renders what a sub-agent run cost, appended to its result so
// the parent — and the user reading the tool result — can see how much work a
// delegation took. Input counts every prompt token the child sent, cached or
// not, summed over its turns.
func subagentUsage(res Result, elapsed time.Duration) string {
	in := res.InputTokens + res.CacheReadInputTokens + res.CacheCreationInputTokens
	return fmt.Sprintf("\n\n<usage>turns: %d\ninput_tokens: %d\noutput_tokens: %d\nduration_ms: %d</usage>",
		res.NumTurns, in, res.OutputTokens, elapsed.Milliseconds())
}
