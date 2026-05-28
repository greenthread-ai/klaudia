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
	Multiline  bool   `json:"multiline,omitempty" jsonschema:"description=Allow patterns to span lines (dot matches newline)"`
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
		"\"count\" shows per-file match counts. Filter files with glob, ignore case with -i.", nil
}

func (g *Grep) InputSchema() json.RawMessage { return g.schema.Raw }

func (g *Grep) ValidateInput(raw json.RawMessage) error { return g.schema.Validate(raw) }

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
	}

	matches, err := search.Grep(search.GrepOptions{
		Pattern:    in.Pattern,
		Root:       root,
		IgnoreCase: in.IgnoreCase,
		Multiline:  in.Multiline,
		Glob:       in.Glob,
	})
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	if len(matches) == 0 {
		return []Result{{Content: "No matches found"}}, nil
	}

	switch in.OutputMode {
	case "content":
		return []Result{{Content: formatContent(matches, in.LineNum)}}, nil
	case "count":
		return []Result{{Content: formatCount(matches)}}, nil
	default: // files_with_matches
		return []Result{{Content: formatFiles(matches)}}, nil
	}
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

// formatContent returns "file:line:text" (or "file:text" without line numbers).
func formatContent(matches []search.GrepMatch, lineNum bool) string {
	var b strings.Builder
	for _, m := range matches {
		if lineNum && m.Line > 0 {
			fmt.Fprintf(&b, "%s:%d:%s\n", m.File, m.Line, m.Text)
		} else {
			fmt.Fprintf(&b, "%s:%s\n", m.File, m.Text)
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
