// Package skill loads reusable prompt/command skills from Markdown files with
// YAML frontmatter. Skills are read from ~/.claude/skills, ~/.klaudia/skills,
// <cwd>/.claude/skills and <cwd>/.klaudia/skills, in that order of increasing
// precedence — the same user-then-project overlay as config.Load and
// mcp.LoadConfig, extended to the directories the wider ecosystem installs
// into.
//
// A skill file looks like:
//
//	---
//	name: review
//	description: Structured review of the current diff
//	type: prompt        # prompt | command
//	tools: [Bash, Read] # optional allowlist (growth point; unused in v1)
//	---
//	Review the staged changes carefully. $ARGUMENTS
//
// The body supports $ARGUMENTS substitution when the skill is invoked.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill types.
const (
	TypePrompt  = "prompt"  // body is injected as instructions for the model
	TypeCommand = "command" // body is a command template (v1: same handling as prompt)
)

// Skill is one loaded skill definition.
type Skill struct {
	Name        string   // invocation name (also the /<name> slash command)
	Description string   // one-line, model-facing
	Type        string   // TypePrompt (default) | TypeCommand
	Tools       []string // optional tool allowlist (growth point; unused in v1)
	Body        string   // template body; supports $ARGUMENTS
	Path        string   // source file, for diagnostics
}

// frontmatter is the YAML header schema.
type frontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Type        string   `yaml:"type"`
	Tools       []string `yaml:"tools"`
}

// Render substitutes $ARGUMENTS in the body with args (the text after the skill
// name). When the body contains no $ARGUMENTS placeholder and args is non-empty,
// the args are appended on a new line so they are never silently dropped.
func (s Skill) Render(args string) string {
	if strings.Contains(s.Body, "$ARGUMENTS") {
		return strings.ReplaceAll(s.Body, "$ARGUMENTS", args)
	}
	if strings.TrimSpace(args) == "" {
		return s.Body
	}
	return strings.TrimRight(s.Body, "\n") + "\n\n" + args
}

// Load reads skills from ~/.klaudia/skills then overlays <cwd>/.klaudia/skills
// (project skills win on name collision). Malformed files are skipped, reporting
// the reason to warn (warn may be nil). The result is sorted by name.
func Load(cwd string, warn func(string)) []Skill {
	byName := map[string]Skill{}

	// Searched in increasing precedence. ~/.claude and .claude are read for
	// the same reason prompt.go reads ~/.claude/CLAUDE.md: that is where the
	// ecosystem's skill installers put things, and a skill someone already has
	// should work here without being moved. Klaudia's own directory wins at
	// each level, so a project can override an installed skill by name.
	dirs := make([]string, 0, 4)
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs,
			filepath.Join(home, ".claude", "skills"),
			filepath.Join(home, ".klaudia", "skills"),
		)
	}
	dirs = append(dirs,
		filepath.Join(cwd, ".claude", "skills"),
		filepath.Join(cwd, ".klaudia", "skills"),
	)

	for _, dir := range dirs {
		for _, sk := range loadDir(dir, warn) {
			byName[sk.Name] = sk // last write (project) wins
		}
	}

	out := make([]Skill, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// loadDir parses every *.md file in dir. A missing dir yields nothing.
// loadDir reads both supported layouts:
//
//	<skills>/<name>.md         — one file per skill
//	<skills>/<name>/SKILL.md   — one directory per skill, so a skill can keep
//	                             supporting files (templates, scripts) beside it
//
// The directory form is the layout skills are increasingly published in, and
// its failure mode used to be silent: subdirectories were skipped before
// anything was parsed, so a correctly written skill in the wrong shape produced
// no skill, no warning, and no Skill tool at all.
func loadDir(dir string, warn func(string)) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // missing/unreadable dir is not an error
	}
	var out []Skill
	for _, e := range entries {
		var (
			path        string
			defaultName string
		)
		switch {
		case e.IsDir():
			path, defaultName = skillFileIn(filepath.Join(dir, e.Name())), e.Name()
			if path == "" {
				warnf(warn, "skill %s: directory has no SKILL.md", filepath.Join(dir, e.Name()))
				continue
			}
		case strings.HasSuffix(e.Name(), ".md"):
			path = filepath.Join(dir, e.Name())
			defaultName = strings.TrimSuffix(e.Name(), ".md")
		default:
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			warnf(warn, "skill %s: %v", path, err)
			continue
		}
		sk, err := parseNamed(data, path, defaultName)
		if err != nil {
			warnf(warn, "skill %s: %v", path, err)
			continue
		}
		out = append(out, sk)
	}
	return out
}

// skillFileIn returns the skill definition inside a skill directory, or "" if
// there is none. Both spellings are accepted because a case-insensitive
// filesystem hides the difference until the file reaches Linux.
func skillFileIn(dir string) string {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func parse(data []byte, path string) (Skill, error) {
	return parseNamed(data, path, strings.TrimSuffix(filepath.Base(path), ".md"))
}

// parseNamed parses a skill, falling back to defaultName when the frontmatter
// omits one — the file's basename for the flat layout, the directory's name for
// the SKILL.md layout, where "SKILL" would be a useless name.
func parseNamed(data []byte, path, defaultName string) (Skill, error) {
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return Skill{}, err
	}
	var meta frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		return Skill{}, fmt.Errorf("invalid frontmatter: %w", err)
	}

	name := strings.TrimSpace(meta.Name)
	if name == "" {
		name = defaultName
	}
	typ := strings.TrimSpace(meta.Type)
	switch typ {
	case "":
		typ = TypePrompt
	case TypePrompt, TypeCommand:
		// ok
	default:
		return Skill{}, fmt.Errorf("invalid type %q (want %q or %q)", typ, TypePrompt, TypeCommand)
	}

	return Skill{
		Name:        name,
		Description: strings.TrimSpace(meta.Description),
		Type:        typ,
		Tools:       meta.Tools,
		Body:        strings.TrimSpace(string(body)),
		Path:        path,
	}, nil
}

// splitFrontmatter separates a leading `---\n … \n---` YAML block from the body.
// A file with no frontmatter returns empty frontmatter and the whole content as
// body (a bare-Markdown skill is valid; its name comes from the filename).
func splitFrontmatter(data []byte) (front, body []byte, err error) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return nil, data, nil
	}
	// Drop the opening fence, then find the closing one at a line start.
	rest := s[strings.IndexByte(s, '\n')+1:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return nil, nil, fmt.Errorf("unterminated frontmatter (missing closing ---)")
	}
	front = []byte(rest[:idx])
	after := rest[idx+len("\n---"):]
	// Skip to the end of the closing fence line.
	if nl := strings.IndexByte(after, '\n'); nl >= 0 {
		after = after[nl+1:]
	} else {
		after = ""
	}
	return front, []byte(after), nil
}

func warnf(warn func(string), format string, args ...any) {
	if warn != nil {
		warn(fmt.Sprintf(format, args...))
	}
}
