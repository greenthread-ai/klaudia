// Package prompt assembles Klaudia's system prompt: base agent instructions, a
// security clause, live environment context (cwd, git, platform, date), and any
// CLAUDE.md project instructions. A richer prompt makes Klaudia a materially
// better coding agent — important for self-hosting (using Klaudia to develop
// Klaudia).
package prompt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// securityClause mirrors the JS constant V44 (05-app-core.js:65659).
const securityClause = "IMPORTANT: Assist with authorized security testing, defensive security, " +
	"CTF challenges, and educational contexts. Refuse requests for destructive techniques, " +
	"DoS attacks, mass targeting, supply chain compromise, or detection evasion for malicious " +
	"purposes. Dual-use security tools (C2 frameworks, credential testing, exploit development) " +
	"require clear authorization context: pentesting engagements, CTF competitions, security " +
	"research, or defensive use cases."

// base is the core agent instruction block.
const base = `You are Klaudia, an interactive CLI agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user.

` + securityClause + `

# Tone and style
Be concise and direct. Output text to communicate with the user; all text you emit outside of tool calls is shown to them. Use GitHub-flavored Markdown. Avoid unnecessary preamble or postamble — answer the question or do the task, then stop.

# Doing tasks
- Use the search tools (Glob, Grep) and Read to understand the codebase and the user's request before making changes.
- Prefer Edit over Write for modifying existing files; read a file before editing it.
- After making code changes, run the project's build/tests via Bash when available to verify your work.
- Follow existing conventions in the codebase. Do not add comments that merely restate the code.
- Only do what was asked; nothing more, nothing less. Do not commit changes unless the user asks.

# Tool use
- When a task needs multiple independent searches or reads, batch them.
- Never guess or fabricate file contents, APIs, or URLs — verify by reading.

# Autonomy and the user's machine
Finish the job. Inside the project you work on your own: read, edit, build, test, run dev servers, use git, install project dependencies, fetch things, and do the destructive parts too — deleting build output and resetting the working tree are ordinary. Work on a remote host or in a container that the task calls for is equally yours to do. Do not stop to ask permission for any of that.

The machine you are running on is different. Changing it — writing outside the project (/etc, /usr, /opt, /Library, shell rc files), installing or removing system packages, controlling services, users, firewall, mounts or kernel parameters — needs the user's agreement first. Use RequestHostChange to describe the whole operation before you start it, in the terms the user cares about and with the reason it is needed. One declaration covers every step inside the scope you describe, so declare the operation, not the first command of it.

If the user declines, do not look for another route to the same change. Carry on with the rest of the task and tell them what you could not do.`

// System builds the full system prompt for a run in the given working directory.
// model is the model ID/alias (may be empty).
func System(cwd, model string) string {
	var b strings.Builder
	b.WriteString(base)
	if model != "" {
		fmt.Fprintf(&b, "\n\nYou are powered by the model %s.", model)
	}
	b.WriteString("\n\n")
	b.WriteString(envBlock(cwd))
	if instr := loadProjectInstructions(cwd); instr != "" {
		b.WriteString("\n\n# Project instructions (from CLAUDE.md)\n")
		b.WriteString(instr)
	}
	if mem := recalledMemory(cwd); mem != "" {
		b.WriteString("\n\n# Recalled memory\n")
		b.WriteString("These are notes you saved in earlier sessions. Use the Memory tool to search for more or to add new ones.\n\n")
		b.WriteString(mem)
	}
	if kn := recalledKnowledge(cwd); kn != "" {
		b.WriteString("\n\n# Project knowledge\n")
		b.WriteString("Curated, durable lessons about this project (from .klaudia/KNOWLEDGE.md). Treat these as established facts.\n\n")
		b.WriteString(kn)
	}
	return b.String()
}

// recalledKnowledge returns the curated project-knowledge file for priming the
// model, or "" if there is none. Sibling of recalledMemory; KNOWLEDGE.md holds
// hand-curated, durable lessons distinct from the free-form memory index.
func recalledKnowledge(cwd string) string {
	data, err := os.ReadFile(filepath.Join(cwd, ".klaudia", "KNOWLEDGE.md"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// recalledMemory returns the project memory index for priming the model, or ""
// if there is none. MEMORY.md is the index: it holds the session bullets and a
// "## Linked memory" section pointing at the .klaudia/memory/*.md detail notes
// (maintained by memory.Store.SyncLinks). The detail notes themselves are not
// inlined — the model opens one (Read / Memory search) only when it's relevant,
// so recall stays cheap as memory grows.
func recalledMemory(cwd string) string {
	klaudiaDir := filepath.Join(cwd, ".klaudia")
	parts := readMarkdownFiles(filepath.Join(klaudiaDir, "MEMORY.md"))

	// Backward compatibility for projects that still have the old session-memory
	// index under .klaudia/memory/MEMORY.md.
	parts = append(parts, readMarkdownFiles(filepath.Join(klaudiaDir, "memory", "MEMORY.md"))...)

	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func readMarkdownFiles(paths ...string) []string {
	var parts []string
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if contents := strings.TrimSpace(string(data)); contents != "" {
			parts = append(parts, contents)
		}
	}
	return parts
}

// envBlock renders the <env> context the model sees each session.
func envBlock(cwd string) string {
	isRepo, branch := gitInfo(cwd)
	repo := "No"
	if isRepo {
		repo = "Yes"
	}
	var b strings.Builder
	b.WriteString("Here is useful information about the environment you are running in:\n<env>\n")
	fmt.Fprintf(&b, "Working directory: %s\n", cwd)
	fmt.Fprintf(&b, "Is directory a git repo: %s\n", repo)
	if isRepo && branch != "" {
		fmt.Fprintf(&b, "Current git branch: %s\n", branch)
	}
	fmt.Fprintf(&b, "Platform: %s\n", runtime.GOOS)
	fmt.Fprintf(&b, "OS Version: %s\n", osVersion())
	fmt.Fprintf(&b, "Today's date: %s\n", time.Now().Format("2006-01-02"))
	b.WriteString("</env>")
	return b.String()
}

// gitInfo reports whether cwd is in a git repo and the current branch.
func gitInfo(cwd string) (bool, string) {
	out, err := runGit(cwd, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(out) != "true" {
		return false, ""
	}
	branch, _ := runGit(cwd, "rev-parse", "--abbrev-ref", "HEAD")
	return true, strings.TrimSpace(branch)
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}

// osVersion returns a best-effort OS version string.
func osVersion() string {
	if out, err := exec.Command("uname", "-sr").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return runtime.GOOS
}

// loadProjectInstructions concatenates CLAUDE.md from the user global config,
// the git root, and cwd (closest last so it takes precedence in the reader's
// mind). Missing files are skipped; duplicates are de-duplicated.
func loadProjectInstructions(cwd string) string {
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".claude", "CLAUDE.md"))
	}
	if root, err := runGit(cwd, "rev-parse", "--show-toplevel"); err == nil {
		paths = append(paths, filepath.Join(strings.TrimSpace(root), "CLAUDE.md"))
	}
	paths = append(paths, filepath.Join(cwd, "CLAUDE.md"))

	seen := map[string]bool{}
	var parts []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		data, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(string(data)); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}
