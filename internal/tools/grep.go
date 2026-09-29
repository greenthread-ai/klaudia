package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/search"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// GrepInput is the Grep tool's input. Mirrors the JS Grep tool's core options.
type GrepInput struct {
	Pattern    string `json:"pattern" jsonschema:"description=The regular expression pattern to search for in file contents"`
	Path       string `json:"path,omitempty" jsonschema:"description=File or directory to search in (defaults to cwd)"`
	Glob       string `json:"glob,omitempty" jsonschema:"description=Glob pattern to filter files (e.g. *.go)"`
	OutputMode string `json:"output_mode,omitempty" jsonschema:"description=files_with_matches (default), content, or count"`
	IgnoreCase bool   `json:"-i,omitempty" jsonschema:"description=Case-insensitive search"`
	LineNum    bool   `json:"-n,omitempty" jsonschema:"description=Show line numbers (content mode)"`
	Multiline  bool   `json:"multiline,omitempty" jsonschema:"description=Allow patterns to span lines (dot matches newline); content mode then shows each match with the line it starts on"`
	Type       string `json:"type,omitempty" jsonschema:"description=Only search files of this type: go, py, js, ts, rust, java, c, cpp, rb, php, sh, md, json, yaml, toml, html, css, sql, swift, kotlin"`
	After      int    `json:"-A,omitempty" jsonschema:"description=Lines of context after each match (content mode)"`
	Before     int    `json:"-B,omitempty" jsonschema:"description=Lines of context before each match (content mode)"`
	Context    int    `json:"-C,omitempty" jsonschema:"description=Lines of context before and after each match (content mode)"`
	HeadLimit  int    `json:"head_limit,omitempty" jsonschema:"description=Return at most this many lines/files/counts; the rest are counted, not shown"`
}

// grepTypes maps the type filter's names to file extensions — the common
// ripgrep types, enough that the model need not spell out a glob.
var grepTypes = map[string][]string{
	"go": {".go"}, "py": {".py", ".pyi"}, "js": {".js", ".mjs", ".cjs", ".jsx"},
	"ts": {".ts", ".tsx", ".mts", ".cts"}, "rust": {".rs"}, "java": {".java"},
	"c": {".c", ".h"}, "cpp": {".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx", ".h"},
	"rb": {".rb"}, "php": {".php"}, "sh": {".sh", ".bash", ".zsh"},
	"md": {".md", ".markdown"}, "json": {".json"}, "yaml": {".yaml", ".yml"},
	"toml": {".toml"}, "html": {".html", ".htm"}, "css": {".css", ".scss", ".sass"},
	"sql": {".sql"}, "swift": {".swift"}, "kotlin": {".kt", ".kts"},
}

// Grep searches file contents using regular expressions.
type Grep struct {
	schema *schema.Schema
}

// NewGrep constructs the Grep tool.
func NewGrep() (*Grep, error) {
	s, err := schema.For[GrepInput]()
	if err != nil {
		return nil, fmt.Errorf("grep: build schema: %w", err)
	}
	return &Grep{schema: s}, nil
}

func (g *Grep) Name() string { return "Grep" }

func (g *Grep) Description(context.Context) (string, error) {
	return "Search file contents with a regular expression. output_mode controls results: " +
		"\"files_with_matches\" (default) lists matching files, \"content\" shows matching lines, " +
		"\"count\" shows per-file match counts. Filter files with glob or type (e.g. type=go), ignore case with -i. " +
		"In content mode, -A/-B/-C add lines of context (context lines use '-' where matches use ':'). " +
		"head_limit caps how many results are shown; the rest are counted. " +
		"A relative path is taken from the working directory.", nil
}

func (g *Grep) InputSchema() json.RawMessage { return g.schema.Raw }

func (g *Grep) ValidateInput(raw json.RawMessage) error {
	if err := g.schema.Validate(raw); err != nil {
		return err
	}
	var in GrepInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if in.Type != "" {
		if _, ok := grepTypes[strings.ToLower(in.Type)]; !ok {
			names := make([]string, 0, len(grepTypes))
			for n := range grepTypes {
				names = append(names, n)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown type %q; known types: %s (or use glob)", in.Type, strings.Join(names, ", "))
		}
	}
	if in.After < 0 || in.Before < 0 || in.Context < 0 || in.HeadLimit < 0 {
		return fmt.Errorf("-A, -B, -C and head_limit must not be negative")
	}
	return nil
}

// PermissionRequest: Grep is read-only.
func (g *Grep) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (g *Grep) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (g *Grep) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in GrepInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	root := in.Path
	if root == "" {
		root = tctx.WorkingDir
	} else {
		root = resolvePath(tctx, root)
	}
	before, after := in.Before, in.After
	if in.Context > 0 {
		before, after = max(before, in.Context), max(after, in.Context)
	}
	if in.OutputMode != "content" {
		before, after = 0, 0 // context only means something where lines are shown
	}

	matches, err := search.Grep(search.GrepOptions{
		Pattern:    in.Pattern,
		Root:       root,
		IgnoreCase: in.IgnoreCase,
		Multiline:  in.Multiline,
		Glob:       in.Glob,
		Exts:       grepTypes[strings.ToLower(in.Type)],
		Before:     before,
		After:      after,
	})
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	if len(matches) == 0 {
		return []Result{{Content: "No matches found"}}, nil
	}

	var out string
	switch in.OutputMode {
	case "content":
		out = formatContent(matches, in.LineNum, before > 0 || after > 0)
	case "count":
		out = formatCount(matches)
	default: // files_with_matches
		out = formatFiles(matches)
	}
	return []Result{{Content: headLimit(out, in.HeadLimit)}}, nil
}

// headLimit keeps the first n lines of out and says how many were left out,
// so a capped result can't be mistaken for a complete one.
func headLimit(out string, n int) string {
	if n <= 0 {
		return out
	}
	lines := strings.Split(out, "\n")
	if len(lines) <= n {
		return out
	}
	return strings.Join(lines[:n], "\n") +
		fmt.Sprintf("\n[showing %d of %d lines; raise head_limit or narrow the search for the rest]", n, len(lines))
}

// formatFiles returns the distinct matching files, in stable order.
func formatFiles(matches []search.GrepMatch) string {
	seen := map[string]bool{}
	var files []string
	for _, m := range matches {
		if !seen[m.File] {
			seen[m.File] = true
			files = append(files, m.File)
		}
	}
	return strings.Join(files, "\n")
}

// formatContent returns "file:line:text" (or "file:text" without line
// numbers). Context lines use '-' in place of ':', as grep and ripgrep do, and
// with context on, a "--" line separates groups that are not adjacent.
func formatContent(matches []search.GrepMatch, lineNum, withContext bool) string {
	var b strings.Builder
	prevFile, prevLine := "", 0
	for _, m := range matches {
		if withContext && prevFile != "" && (m.File != prevFile || m.Line != prevLine+1) {
			b.WriteString("--\n")
		}
		prevFile, prevLine = m.File, m.Line
		sep := ":"
		if m.Context {
			sep = "-"
		}
		if lineNum && m.Line > 0 {
			fmt.Fprintf(&b, "%s%s%d%s%s\n", m.File, sep, m.Line, sep, m.Text)
		} else {
			fmt.Fprintf(&b, "%s%s%s\n", m.File, sep, m.Text)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatCount returns "file:count" lines, sorted by file.
func formatCount(matches []search.GrepMatch) string {
	counts := map[string]int{}
	for _, m := range matches {
		counts[m.File]++
	}
	files := make([]string, 0, len(counts))
	for f := range counts {
		files = append(files, f)
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s:%d\n", f, counts[f])
	}
	return strings.TrimRight(b.String(), "\n")
}
