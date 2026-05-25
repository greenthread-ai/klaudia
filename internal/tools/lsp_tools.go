package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/greenthread/klaudia/internal/lsp"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// resolvePath makes a tool-supplied path absolute relative to the working dir.
func resolvePath(tctx Context, p string) string {
	if filepath.IsAbs(p) || tctx.WorkingDir == "" {
		return p
	}
	return filepath.Join(tctx.WorkingDir, p)
}

// --- Diagnostics ---

type DiagnosticsInput struct {
	File string `json:"file" jsonschema:"description=Path to the source file to type-check (relative to the working directory or absolute)"`
}

// Diagnostics reports compiler/linter problems for a file via its language
// server — the loop to use after editing: edit, then check, then fix.
type Diagnostics struct {
	schema *schema.Schema
	pool   *lsp.Pool
}

func NewDiagnostics(pool *lsp.Pool) (*Diagnostics, error) {
	s, err := schema.For[DiagnosticsInput]()
	if err != nil {
		return nil, fmt.Errorf("diagnostics: build schema: %w", err)
	}
	return &Diagnostics{schema: s, pool: pool}, nil
}

func (t *Diagnostics) Name() string { return "Diagnostics" }

func (t *Diagnostics) Description(context.Context) (string, error) {
	return "Report compiler/linter diagnostics (errors and warnings) for a source file using its language " +
		"server (e.g. gopls for Go). Use this after editing a file to verify your change compiles and to see " +
		"exact error locations, then fix them.", nil
}

func (t *Diagnostics) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *Diagnostics) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in DiagnosticsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.File) == "" {
		return fmt.Errorf("file is required")
	}
	return nil
}

func (t *Diagnostics) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

// Diagnostics is read-only analysis via a local dev tool.
func (t *Diagnostics) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *Diagnostics) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in DiagnosticsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.pool == nil {
		return []Result{{Content: "language servers are not available", IsError: true}}, nil
	}
	path := resolvePath(tctx, in.File)
	diags, err := t.pool.Diagnostics(ctx, path)
	if err != nil {
		return []Result{{Content: "Error: " + err.Error(), IsError: true}}, nil
	}
	if len(diags) == 0 {
		return []Result{{Content: "No diagnostics — " + in.File + " is clean."}}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d diagnostic(s) for %s:\n", len(diags), in.File)
	for _, d := range diags {
		// LSP positions are 0-based; show 1-based for humans.
		fmt.Fprintf(&b, "  [%s] %d:%d  %s", lsp.SeverityName(d.Severity), d.Range.Start.Line+1, d.Range.Start.Character+1, d.Message)
		if d.Source != "" {
			fmt.Fprintf(&b, "  (%s)", d.Source)
		}
		b.WriteString("\n")
	}
	return []Result{{Content: strings.TrimRight(b.String(), "\n")}}, nil
}

// --- Definition / References (shared) ---

type locInput struct {
	File      string `json:"file" jsonschema:"description=Path to the source file (relative to the working directory or absolute)"`
	Line      int    `json:"line" jsonschema:"description=1-based line number of the symbol"`
	Character int    `json:"character" jsonschema:"description=1-based column of the symbol on that line"`
}

// lspLocTool implements Definition and References (same input + output shape).
type lspLocTool struct {
	name   string
	desc   string
	schema *schema.Schema
	pool   *lsp.Pool
	query  func(ctx context.Context, p *lsp.Pool, path string, pos lsp.Position) ([]lsp.Location, error)
}

func NewDefinition(pool *lsp.Pool) (Tool, error) {
	return newLocTool("Definition",
		"Find where the symbol at a file position is defined, via the language server. "+
			"Line and character are 1-based. Returns the definition location(s).",
		pool, func(ctx context.Context, p *lsp.Pool, path string, pos lsp.Position) ([]lsp.Location, error) {
			return p.Definition(ctx, path, pos)
		})
}

func NewReferences(pool *lsp.Pool) (Tool, error) {
	return newLocTool("References",
		"Find all references to the symbol at a file position, via the language server. "+
			"Line and character are 1-based. Returns every usage location.",
		pool, func(ctx context.Context, p *lsp.Pool, path string, pos lsp.Position) ([]lsp.Location, error) {
			return p.References(ctx, path, pos)
		})
}

func newLocTool(name, desc string, pool *lsp.Pool, query func(context.Context, *lsp.Pool, string, lsp.Position) ([]lsp.Location, error)) (Tool, error) {
	s, err := schema.For[locInput]()
	if err != nil {
		return nil, fmt.Errorf("%s: build schema: %w", strings.ToLower(name), err)
	}
	return &lspLocTool{name: name, desc: desc, schema: s, pool: pool, query: query}, nil
}

func (t *lspLocTool) Name() string                                { return t.name }
func (t *lspLocTool) Description(context.Context) (string, error) { return t.desc, nil }
func (t *lspLocTool) InputSchema() json.RawMessage                { return t.schema.Raw }

func (t *lspLocTool) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in locInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.File) == "" || in.Line < 1 {
		return fmt.Errorf("file and a 1-based line are required")
	}
	return nil
}

func (t *lspLocTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *lspLocTool) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *lspLocTool) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in locInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.pool == nil {
		return []Result{{Content: "language servers are not available", IsError: true}}, nil
	}
	path := resolvePath(tctx, in.File)
	// Convert the 1-based input to LSP's 0-based positions.
	col := in.Character - 1
	if col < 0 {
		col = 0
	}
	locs, err := t.query(ctx, t.pool, path, lsp.Position{Line: in.Line - 1, Character: col})
	if err != nil {
		return []Result{{Content: "Error: " + err.Error(), IsError: true}}, nil
	}
	if len(locs) == 0 {
		return []Result{{Content: "No results."}}, nil
	}
	var b strings.Builder
	for _, l := range locs {
		fmt.Fprintf(&b, "%s:%d:%d\n", lsp.URIPath(l.URI), l.Range.Start.Line+1, l.Range.Start.Character+1)
	}
	return []Result{{Content: strings.TrimRight(b.String(), "\n")}}, nil
}
