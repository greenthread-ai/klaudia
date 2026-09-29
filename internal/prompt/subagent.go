package prompt

import (
	"os"
	"strings"
)

// Subagent builds a sub-agent's system prompt: the type's own prompt, then the
// environment block and the project's instructions and knowledge — the same
// context the main agent is given.
//
// The type prompt alone left a general-purpose sub-agent editing code with no
// idea of the working directory, the platform, or the project's rules, so it
// ignored CLAUDE.md the moment work was delegated to it. Recalled memory is
// left out: it is the parent session's running notes rather than project
// rules, and the parent can pass on whatever of it matters in the prompt.
//
// cwd is the directory the sub-agent's tools run in; empty means the process
// working directory, which is where they run in that case.
func Subagent(typePrompt, cwd string) string {
	if cwd == "" {
		if wd, err := os.Getwd(); err == nil {
			cwd = wd
		}
	}
	var b strings.Builder
	b.WriteString(typePrompt)
	b.WriteString("\n\n")
	b.WriteString(envBlock(cwd))
	if instr := loadProjectInstructions(cwd); instr != "" {
		b.WriteString("\n\n# Project instructions (from CLAUDE.md)\n")
		b.WriteString(instr)
	}
	if kn := recalledKnowledge(cwd); kn != "" {
		b.WriteString("\n\n# Project knowledge\n")
		b.WriteString("Curated, durable lessons about this project (from .klaudia/KNOWLEDGE.md). Treat these as established facts.\n\n")
		b.WriteString(kn)
	}
	return b.String()
}
