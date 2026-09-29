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
//	type: prompt              # prompt | command
//	allowed-tools: [Bash, Read] # optional tool allowlist (stored; not yet enforced)
//	argument-hint: <path>     # optional hint shown in the Skill tool description
//	---
//	Review the staged changes carefully. $ARGUMENTS
//
// The body supports argument substitution when the skill is invoked:
//
//	$ARGUMENTS         all invocation arguments, verbatim
//	$1, $2, … $N       the Nth whitespace-separated argument ("" when absent)
//	${CLAUDE_SKILL_DIR} the skill's directory, for referencing bundled files
//	                   (also ${KLAUDIA_SKILL_DIR}, and either without braces)
//
// Frontmatter parity note: `allowed-tools` and `argument-hint` mirror Claude
// Code's skill frontmatter. `allowed-tools` is the preferred spelling; the
// older `tools` key is still accepted as an alias. `model` is not read: there
// is no per-skill model switch for it to drive.
package skill

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/greenthread-ai/klaudia/internal/session"
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
	Tools       []string // tool allowlist from allowed-tools (alias: tools); stored/surfaced, not yet enforced
	ArgHint     string   // argument-hint: shown in completion/help; surfaced in the Skill tool description
	Body        string   // template body; supports $ARGUMENTS, $1..$N and ${CLAUDE_SKILL_DIR}
	Path        string   // source file; its directory is the skill's (Dir)
}

// frontmatter is the YAML header schema.
type frontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Type         string   `yaml:"type"`
	Tools        []string `yaml:"tools"`         // legacy alias for allowed-tools
	AllowedTools []string `yaml:"allowed-tools"` // Claude Code spelling; wins over tools
	ArgHint      string   `yaml:"argument-hint"`
}

// positionalRe matches a positional argument placeholder: $1, $2, … $12. The
// digit run is captured so multi-digit indices ($10) are not mistaken for $1
// followed by a literal 0.
var positionalRe = regexp.MustCompile(`\$(\d+)`)

// skillDirRe matches the skill-directory placeholders: ${CLAUDE_SKILL_DIR}
// (the form skills written for Claude Code use) and ${KLAUDIA_SKILL_DIR},
// braced or bare.
var skillDirRe = regexp.MustCompile(`\$\{(?:CLAUDE|KLAUDIA)_SKILL_DIR\}|\$(?:CLAUDE|KLAUDIA)_SKILL_DIR\b`)

// Dir is the directory holding the skill file and anything bundled beside it,
// or "" for a skill with no source file.
func (s Skill) Dir() string {
	if s.Path == "" {
		return ""
	}
	return filepath.Dir(s.Path)
}

// Render expands the skill body for one invocation and returns the text handed
// to the model (as a Skill tool result) or submitted as the /<name> prompt.
//
// Substitutions:
//   - $1, $2, … $N → the Nth whitespace-separated argument, "" when absent
//   - $ARGUMENTS → the full args string, verbatim
//   - ${CLAUDE_SKILL_DIR} / ${KLAUDIA_SKILL_DIR} (braced or bare) → the
//     skill's directory
//
// Positionals are expanded first, on the body as written, so a "$1" inside
// the arguments themselves is left alone. When the body references none of
// $ARGUMENTS or $1..$N and args is non-empty, the args are appended on a new
// line so they are never silently dropped. Finally, when the skill has a known
// directory, a short preamble names it so the model can locate bundled files
// even if the body never mentions the placeholder.
func (s Skill) Render(args string) string {
	body := s.Body
	usesArgs := false
	if positionalRe.MatchString(body) {
		usesArgs = true
		fields := strings.Fields(args)
		body = positionalRe.ReplaceAllStringFunc(body, func(m string) string {
			n, err := strconv.Atoi(m[1:])
			if err != nil || n < 1 || n > len(fields) {
				return ""
			}
			return fields[n-1]
		})
	}
	if strings.Contains(body, "$ARGUMENTS") {
		usesArgs = true
		body = strings.ReplaceAll(body, "$ARGUMENTS", args)
	}
	if !usesArgs && strings.TrimSpace(args) != "" {
		body = strings.TrimRight(body, "\n") + "\n\n" + args
	}
	dir := s.Dir()
	if dir == "" {
		return body
	}
	body = skillDirRe.ReplaceAllLiteralString(body, dir)
	return "Skill base directory: " + dir +
		"\n(Bundled files live here; ${CLAUDE_SKILL_DIR} expands to this path.)\n\n" + body
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
	if home, err := os.UserHomeDir(); err == nil && !session.Restricted() {
		dirs = append(dirs, filepath.Join(home, ".claude", "skills"))
	}
	// Klaudia's own user directory is the config root, so KLAUDIA_CONFIG_DIR
	// moves it with everything else.
	dirs = append(dirs, filepath.Join(session.ConfigRoot(), "skills"))
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
// loadDir reads the two layouts a skill is written in:
//
//	<skills>/<name>.md         — one file per skill
//	<skills>/<name>/SKILL.md   — one directory per skill, so it can keep
//	                             templates, licences or scripts alongside
//
// It does not descend further, which is a trust boundary rather than a
// limitation. Claude's synced skills arrive at
// <skills>/synced/<workspace-id>/<name>/SKILL.md, placed there by a backend
// and able to change between runs; a skill is instructions injected into the
// model's context, so loading whatever appeared overnight is not a courtesy we
// extend. Copy one into .klaudia/skills (or ~/.claude/skills directly) to use
// it, where it sits in version control and changes when you change it.
//
// Hidden directories are skipped for a related reason: <skills>/.trash holds
// skills that were deleted but are still complete on disk, and they should
// stay deleted.
func loadDir(dir string, warn func(string)) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // missing/unreadable dir is not an error
	}
	var out []Skill
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") { // .trash, .git, .DS_Store
			continue
		}

		if !e.IsDir() {
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			if sk, ok := loadSkillFile(filepath.Join(dir, name), strings.TrimSuffix(name, ".md"), warn); ok {
				out = append(out, sk)
			}
			continue
		}

		sub := filepath.Join(dir, name)
		if path := skillFileIn(sub); path != "" {
			if sk, ok := loadSkillFile(path, name, warn); ok {
				out = append(out, sk)
			}
			continue
		}
		// Only complain when the directory looks like an attempt at a skill.
		// A directory of other directories is a container — synced/, or
		// somebody's own filing — and warning about it is pure noise.
		if hasMarkdown(sub) {
			warnf(warn, "skill %s: directory has no SKILL.md", sub)
		}
	}
	return out
}

func loadSkillFile(path, defaultName string, warn func(string)) (Skill, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		warnf(warn, "skill %s: %v", path, err)
		return Skill{}, false
	}
	sk, err := parseNamed(data, path, defaultName)
	if err != nil {
		warnf(warn, "skill %s: %v", path, err)
		return Skill{}, false
	}
	return sk, true
}

// hasMarkdown reports whether a directory directly contains any .md file.
func hasMarkdown(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			return true
		}
	}
	return false
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
	// A UTF-8 byte-order mark (editors on Windows add one) hid the opening
	// "---", so the frontmatter was read as body and the skill lost its
	// description without a word.
	data = bytes.TrimPrefix(data, []byte("\ufeff"))
	fm, body, err := splitFrontmatter(data)
	if err != nil {
		return Skill{}, err
	}
	var meta frontmatter
	if err := yaml.Unmarshal(fm, &meta); err != nil {
		// Skill files are written as "key: text", and text is prose: a
		// description like "Use this. Triggers include: deck" is not valid
		// YAML (a second ": " starts a mapping), and strict parsing turned
		// such skills away. Read the known keys line by line instead, and
		// fail only when that finds nothing either.
		lenient, ok := lenientFrontmatter(fm)
		if !ok {
			return Skill{}, fmt.Errorf("invalid frontmatter: %w", err)
		}
		meta = lenient
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

	// allowed-tools is the Claude Code spelling and wins; tools is the legacy
	// alias kept so existing skills keep working.
	toolset := meta.AllowedTools
	if len(toolset) == 0 {
		toolset = meta.Tools
	}

	return Skill{
		Name:        name,
		Description: strings.TrimSpace(meta.Description),
		Type:        typ,
		Tools:       toolset,
		ArgHint:     strings.TrimSpace(meta.ArgHint),
		Body:        strings.TrimSpace(string(body)),
		Path:        path,
	}, nil
}

// lenientFrontmatter reads "key: rest of line" for the frontmatter keys,
// joining indented continuation lines to the value before them and stripping
// matching quotes. tools and allowed-tools take "[a, b]" or "a, b". ok is
// false when no known key was found.
func lenientFrontmatter(fm []byte) (frontmatter, bool) {
	var meta frontmatter
	vals := map[string]string{}
	var last string
	for _, line := range strings.Split(string(fm), "\n") {
		line = strings.TrimRight(line, "\r")
		if last != "" && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && strings.TrimSpace(line) != "" {
			vals[last] += " " + strings.TrimSpace(line)
			continue
		}
		key, val, found := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		if !found || !slices.Contains([]string{"name", "description", "type", "tools", "allowed-tools", "argument-hint"}, key) {
			last = ""
			continue
		}
		vals[key] = strings.TrimSpace(val)
		last = key
	}
	if len(vals) == 0 {
		return meta, false
	}
	unquote := func(s string) string {
		if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
			return s[1 : len(s)-1]
		}
		return s
	}
	meta.Name = unquote(vals["name"])
	meta.Description = unquote(vals["description"])
	meta.Type = unquote(vals["type"])
	meta.ArgHint = unquote(vals["argument-hint"])
	list := func(v string) []string {
		var out []string
		for _, tool := range strings.Split(strings.Trim(v, "[] "), ",") {
			if tool = unquote(strings.TrimSpace(tool)); tool != "" {
				out = append(out, tool)
			}
		}
		return out
	}
	meta.Tools = list(vals["tools"])
	meta.AllowedTools = list(vals["allowed-tools"])
	return meta, true
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
