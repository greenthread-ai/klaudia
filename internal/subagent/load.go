package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Load returns the built-in types plus the sub-agents defined as markdown
// files, the way Claude Code and its plugins define them:
//
//	---
//	name: code-reviewer
//	description: Reviews a diff for bugs and style
//	tools: Read, Grep, Glob        # or a YAML list; omitted means all tools
//	model: sonnet                  # optional; defaults to the parent's model
//	---
//	You are a code reviewer. …     (the body is the system prompt)
//
// Directories are read in increasing precedence — ~/.claude/agents,
// ~/.klaudia/agents, then the project's .claude/agents and .klaudia/agents —
// and a later definition replaces an earlier one of the same name, a built-in
// included. A file that cannot be parsed is skipped and reported to warn.
//
// There was no way to add one: the three built-ins were fixed, so agent
// definitions written for Claude Code (the review and feature-development
// agents among them) could not be used.
//
// A definition narrows what a sub-agent may do — a prompt and a tool
// allowlist — and runs under the parent's permissions, so project
// definitions are read like CLAUDE.md is.
func Load(cwd string, warn func(string)) []Type {
	byName := map[string]Type{}
	order := []string{}
	put := func(t Type) {
		key := normalize(t.Name)
		if _, ok := byName[key]; !ok {
			order = append(order, key)
		}
		byName[key] = t
	}
	for _, t := range Builtin() {
		put(t)
	}
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".claude", "agents"), filepath.Join(home, ".klaudia", "agents"))
	}
	dirs = append(dirs, filepath.Join(cwd, ".claude", "agents"), filepath.Join(cwd, ".klaudia", "agents"))
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
		sort.Strings(files)
		for _, f := range files {
			t, err := parseAgentFile(f)
			if err != nil {
				if warn != nil {
					warn(fmt.Sprintf("agent %s: %v", f, err))
				}
				continue
			}
			put(t)
		}
	}
	out := make([]Type, 0, len(order))
	for _, k := range order {
		out = append(out, byName[k])
	}
	return out
}

type agentFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Tools       any    `yaml:"tools"`
	Model       string `yaml:"model"`
}

func parseAgentFile(path string) (Type, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Type{}, err
	}
	s := strings.TrimPrefix(string(data), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return Type{}, fmt.Errorf("no frontmatter (want a --- block with name and description)")
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return Type{}, fmt.Errorf("unterminated frontmatter")
	}
	var fm agentFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		// Agent files are written as "key: text" with prose in the text:
		// "description: Use this agent when …" followed by unindented
		// "Context: …" / "user: …" example lines. That is not YAML, and most
		// of the agents shipped in Claude Code's own plugins are written so.
		lenient, ok := lenientAgentFrontmatter(rest[:end])
		if !ok {
			return Type{}, fmt.Errorf("invalid frontmatter: %w", err)
		}
		fm = lenient
	}
	body := rest[end+len("\n---"):]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	} else {
		body = ""
	}
	if strings.EqualFold(strings.TrimSpace(fm.Model), "inherit") {
		fm.Model = "" // Claude Code's spelling of "the parent's model"
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if strings.TrimSpace(fm.Description) == "" {
		return Type{}, fmt.Errorf("missing description (the model chooses agents by it)")
	}
	t := Type{
		Name:         name,
		Description:  strings.TrimSpace(fm.Description),
		SystemPrompt: strings.TrimSpace(body),
		Model:        strings.TrimSpace(fm.Model),
		Tools:        []string{"*"},
	}
	switch v := fm.Tools.(type) {
	case nil:
	case string:
		t.Tools = splitTools(v)
	case []any:
		t.Tools = nil
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				t.Tools = append(t.Tools, strings.TrimSpace(s))
			}
		}
	default:
		return Type{}, fmt.Errorf("tools must be a list or a comma-separated string")
	}
	if len(t.Tools) == 0 {
		t.Tools = []string{"*"}
	}
	if t.SystemPrompt == "" {
		return Type{}, fmt.Errorf("empty body (the body is the agent's system prompt)")
	}
	return t, nil
}

// agentKeys are the frontmatter keys an agent file uses. A line starting with
// one begins a new field; any other line continues the field before it.
var agentKeys = map[string]bool{"name": true, "description": true, "tools": true, "model": true, "color": true}

// lenientAgentFrontmatter reads an agent file's frontmatter as "key: value"
// lines, joining every line that does not start a known key to the value
// before it. ok is false when no known key was found.
func lenientAgentFrontmatter(fm string) (agentFrontmatter, bool) {
	vals := map[string]string{}
	var last string
	for _, line := range strings.Split(fm, "\n") {
		if key, val, found := strings.Cut(line, ":"); found && agentKeys[strings.TrimSpace(key)] && !strings.HasPrefix(line, " ") {
			last = strings.TrimSpace(key)
			vals[last] = strings.TrimSpace(val)
			continue
		}
		if last != "" {
			vals[last] += "\n" + line
		}
	}
	if len(vals) == 0 {
		return agentFrontmatter{}, false
	}
	out := agentFrontmatter{
		Name:        strings.TrimSpace(vals["name"]),
		Description: strings.TrimSpace(vals["description"]),
		Model:       strings.TrimSpace(vals["model"]),
	}
	if t := strings.Trim(strings.TrimSpace(vals["tools"]), "[]"); t != "" {
		out.Tools = t
	}
	return out, true
}

func splitTools(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// normalize folds case and separators, so "explore", "Explore" and
// "general_purpose" name the same agents as "Explore" and "general-purpose".
func normalize(name string) string {
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(strings.TrimSpace(name)))
}

// Find returns the type in types matching name, ignoring case and separators.
func Find(types []Type, name string) (Type, bool) {
	key := normalize(name)
	for _, t := range types {
		if normalize(t.Name) == key {
			return t, true
		}
	}
	return Type{}, false
}
