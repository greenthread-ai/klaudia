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
//	allowed-tools: [Bash, Read] # optional tool allowlist (stored/surfaced; not yet enforced)
//	argument-hint: <path>     # optional hint shown in completion/help
//	model: sonnet             # optional per-skill model hint (stored/documented)
//	---
//	Review the staged changes carefully. $ARGUMENTS
//
// The body supports argument substitution when the skill is invoked:
//
//	$ARGUMENTS         all invocation arguments, verbatim
//	$1, $2, … $N       the Nth whitespace-separated argument ("" when absent)
//	$KLAUDIA_SKILL_DIR the skill's base directory, for referencing bundled files
//	                   (also written ${KLAUDIA_SKILL_DIR})
//
// Frontmatter parity note: `allowed-tools`, `argument-hint` and `model` mirror
// Claude Code's skill/command frontmatter. `allowed-tools` is the preferred
// spelling; the older `tools` key is still accepted as an alias. Substitution
// and the base-directory preamble follow Claude Code semantics.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	Tools       []string // tool allowlist from allowed-tools (alias: tools); stored/surfaced, not yet enforced
	ArgHint     string   // argument-hint: shown in completion/help; surfaced in the Skill tool description
	Model       string   // model: per-skill model hint; stored/documented, no invocation-time switch yet
	Body        string   // template body; supports $ARGUMENTS, $1..$N and $KLAUDIA_SKILL_DIR
	Path        string   // source file, for diagnostics
	Dir         string   // base directory holding the skill and its bundled files
}

// frontmatter is the YAML header schema.
type frontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	Type         string   `yaml:"type"`
	Tools        []string `yaml:"tools"`         // legacy alias for allowed-tools
	AllowedTools []string `yaml:"allowed-tools"` // Claude Code spelling; wins over tools
	ArgHint      string   `yaml:"argument-hint"`
	Model        string   `yaml:"model"`
}

// positionalRe matches a positional argument placeholder: $1, $2, … $12. The
// digit run is captured so multi-digit indices ($10) are not mistaken for $1
// followed by a literal 0.
var positionalRe = regexp.MustCompile(`\$(\d+)`)

// Render expands the skill body for one invocation and returns the text handed
// to the model (as a Skill tool result) or submitted as the /<name> prompt.
//
// Substitutions, in order:
//   - $KLAUDIA_SKILL_DIR / ${KLAUDIA_SKILL_DIR} → the skill's base directory
//   - $ARGUMENTS → the full args string, verbatim
//   - $1, $2, … $N → the Nth whitespace-separated argument, "" when absent
//
// When the body references none of $ARGUMENTS or $1..$N and args is non-empty,
// the args are appended on a new line so they are never silently dropped — the
// original backward-compatible behaviour. Finally, when the skill has a known
// base directory, a short preamble names it so the model can locate bundled
// files even if the body never mentions $KLAUDIA_SKILL_DIR.
func (s Skill) Render(args string) string {
	body := s.Body
	if s.Dir != "" {
		body = strings.ReplaceAll(body, "${KLAUDIA_SKILL_DIR}", s.Dir)
		body = strings.ReplaceAll(body, "$KLAUDIA_SKILL_DIR", s.Dir)
	}

	usesArgs := false
	if strings.Contains(body, "$ARGUMENTS") {
		usesArgs = true
		body = strings.ReplaceAll(body, "$ARGUMENTS", args)
	}
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
	if !usesArgs && strings.TrimSpace(args) != "" {
		body = strings.TrimRight(body, "\n") + "\n\n" + args
	}

	return s.withBaseDir(body)
}

// withBaseDir prepends a one-line note naming the skill's base directory, so a
// skill can reference bundled files (templates, scripts) that live beside it.
// Skills with no known directory (e.g. a bare Skill value in a test) are
// returned unchanged.
func (s Skill) withBaseDir(body string) string {
	if s.Dir == "" {
		return body
	}
	return "Skill base directory: " + s.Dir +
		"\n(Bundled files live here; $KLAUDIA_SKILL_DIR expands to this path.)\n\n" + body
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
		Model:       strings.TrimSpace(meta.Model),
		Body:        strings.TrimSpace(string(body)),
		Path:        path,
		Dir:         filepath.Dir(path),
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
