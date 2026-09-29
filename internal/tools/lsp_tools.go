package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/lsp"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
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

func NewImplementation(pool *lsp.Pool) (Tool, error) {
	return newLocTool("Implementation",
		"Find the implementation(s) of the symbol at a file position, via the language server "+
			"(e.g. the concrete types satisfying an interface, or a method's implementations). "+
			"Line and character are 1-based. Returns each implementation location with its source line.",
		pool, func(ctx context.Context, p *lsp.Pool, path string, pos lsp.Position) ([]lsp.Location, error) {
			return p.Implementation(ctx, path, pos)
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
		return lspErr(t.name, err), nil
	}
	if len(locs) == 0 {
		return []Result{{Content: "No results."}}, nil
	}
	return []Result{{Content: formatLocations(locs)}}, nil
}

// formatLocations renders location results as "path:line:col  <source line>",
// including the actual source text so the model doesn't need a follow-up Read.
func formatLocations(locs []lsp.Location) string {
	var b strings.Builder
	for _, l := range locs {
		fmt.Fprintf(&b, "%s:%d:%d", lsp.URIPath(l.URI), l.Range.Start.Line+1, l.Range.Start.Character+1)
		if src := lsp.SourceLine(l.URI, l.Range.Start.Line); src != "" {
			fmt.Fprintf(&b, "  %s", strings.TrimSpace(src))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// lspErr renders an LSP error, translating an unsupported-capability sentinel
// into a plain "not supported" message rather than an error result.
func lspErr(action string, err error) []Result {
	if errors.Is(err, lsp.ErrNotSupported) {
		return []Result{{Content: action + " is not supported by this file's language server."}}
	}
	return []Result{{Content: "Error: " + err.Error(), IsError: true}}
}

// --- Hover ---

// Hover reports the language server's hover info (type/signature/doc) at a
// position. Reuses locInput (file + 1-based line/character).
type Hover struct {
	schema *schema.Schema
	pool   *lsp.Pool
}

func NewHover(pool *lsp.Pool) (Tool, error) {
	s, err := schema.For[locInput]()
	if err != nil {
		return nil, fmt.Errorf("hover: build schema: %w", err)
	}
	return &Hover{schema: s, pool: pool}, nil
}

func (t *Hover) Name() string { return "Hover" }

func (t *Hover) Description(context.Context) (string, error) {
	return "Show the language server's hover information for the symbol at a file position — its type, " +
		"signature, and documentation (e.g. what gopls shows on hover). Line and character are 1-based.", nil
}

func (t *Hover) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *Hover) ValidateInput(raw json.RawMessage) error {
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

func (t *Hover) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *Hover) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *Hover) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in locInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.pool == nil {
		return []Result{{Content: "language servers are not available", IsError: true}}, nil
	}
	path := resolvePath(tctx, in.File)
	col := in.Character - 1
	if col < 0 {
		col = 0
	}
	h, err := t.pool.Hover(ctx, path, lsp.Position{Line: in.Line - 1, Character: col})
	if err != nil {
		return lspErr("Hover", err), nil
	}
	if h == nil || strings.TrimSpace(h.Text) == "" {
		return []Result{{Content: "No hover information at that position."}}, nil
	}
	var b strings.Builder
	// Anchor the hover to the source line it describes.
	line := in.Line - 1
	if h.Range != nil {
		line = h.Range.Start.Line
	}
	if src := lsp.SourceLine(path, line); src != "" {
		fmt.Fprintf(&b, "%s:%d  %s\n\n", in.File, line+1, strings.TrimSpace(src))
	}
	b.WriteString(strings.TrimSpace(h.Text))
	return []Result{{Content: b.String()}}, nil
}

// --- DocumentSymbols ---

type documentSymbolsInput struct {
	File string `json:"file" jsonschema:"description=Path to the source file (relative to the working directory or absolute)"`
}

// DocumentSymbols lists the symbols declared in a file (functions, types, ...).
type DocumentSymbols struct {
	schema *schema.Schema
	pool   *lsp.Pool
}

func NewDocumentSymbols(pool *lsp.Pool) (Tool, error) {
	s, err := schema.For[documentSymbolsInput]()
	if err != nil {
		return nil, fmt.Errorf("documentsymbols: build schema: %w", err)
	}
	return &DocumentSymbols{schema: s, pool: pool}, nil
}

func (t *DocumentSymbols) Name() string { return "DocumentSymbols" }

func (t *DocumentSymbols) Description(context.Context) (string, error) {
	return "List the symbols declared in a source file — functions, types, methods, variables — with their " +
		"kind and location, via the language server. Use it to get an outline of a file before reading it.", nil
}

func (t *DocumentSymbols) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *DocumentSymbols) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in documentSymbolsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.File) == "" {
		return fmt.Errorf("file is required")
	}
	return nil
}

func (t *DocumentSymbols) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *DocumentSymbols) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *DocumentSymbols) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in documentSymbolsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.pool == nil {
		return []Result{{Content: "language servers are not available", IsError: true}}, nil
	}
	path := resolvePath(tctx, in.File)
	syms, err := t.pool.DocumentSymbols(ctx, path)
	if err != nil {
		return lspErr("DocumentSymbols", err), nil
	}
	if len(syms) == 0 {
		return []Result{{Content: "No symbols found in " + in.File + "."}}, nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d symbol(s) in %s:\n", len(syms), in.File)
	for _, s := range syms {
		fmt.Fprintf(&b, "  [%s] %s", lsp.SymbolKindName(s.Kind), s.Name)
		if s.Container != "" {
			fmt.Fprintf(&b, " (in %s)", s.Container)
		}
		fmt.Fprintf(&b, "  %s:%d", lsp.URIPath(s.Location.URI), s.Location.Range.Start.Line+1)
		if src := lsp.SourceLine(s.Location.URI, s.Location.Range.Start.Line); src != "" {
			fmt.Fprintf(&b, "  %s", strings.TrimSpace(src))
		}
		b.WriteString("\n")
	}
	return []Result{{Content: strings.TrimRight(b.String(), "\n")}}, nil
}

// --- Rename ---

type renameInput struct {
	File      string `json:"file" jsonschema:"description=Path to the source file (relative to the working directory or absolute)"`
	Line      int    `json:"line" jsonschema:"description=1-based line number of the symbol to rename"`
	Character int    `json:"character" jsonschema:"description=1-based column of the symbol on that line"`
	NewName   string `json:"new_name" jsonschema:"description=The new name for the symbol"`
}

// Rename previews a workspace rename: it returns the edits the language server
// would make, grouped by file, WITHOUT applying them (use Edit to apply).
type Rename struct {
	schema *schema.Schema
	pool   *lsp.Pool
}

func NewRename(pool *lsp.Pool) (Tool, error) {
	s, err := schema.For[renameInput]()
	if err != nil {
		return nil, fmt.Errorf("rename: build schema: %w", err)
	}
	return &Rename{schema: s, pool: pool}, nil
}

func (t *Rename) Name() string { return "Rename" }

func (t *Rename) Description(context.Context) (string, error) {
	return "Preview a symbol rename via the language server. Given a symbol's position and a new name, it " +
		"returns every edit the rename would make across the workspace (file, line, and replacement text) " +
		"WITHOUT applying them — review the preview, then use the Edit tool to apply the changes you want. " +
		"Line and character are 1-based.", nil
}

func (t *Rename) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *Rename) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in renameInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.File) == "" || in.Line < 1 || strings.TrimSpace(in.NewName) == "" {
		return fmt.Errorf("file, a 1-based line, and new_name are required")
	}
	return nil
}

func (t *Rename) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

// Rename is read-only: it only computes and returns a preview of the edits.
func (t *Rename) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *Rename) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in renameInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if t.pool == nil {
		return []Result{{Content: "language servers are not available", IsError: true}}, nil
	}
	path := resolvePath(tctx, in.File)
	col := in.Character - 1
	if col < 0 {
		col = 0
	}
	edits, err := t.pool.Rename(ctx, path, lsp.Position{Line: in.Line - 1, Character: col}, in.NewName)
	if err != nil {
		return lspErr("Rename", err), nil
	}
	return []Result{{Content: formatRename(in.NewName, edits)}}, nil
}

// formatRename renders a rename preview: a per-file, per-line list of the
// replacements, plus a total count. It does not apply anything.
func formatRename(newName string, edits []lsp.FileEdits) string {
	total := 0
	for _, fe := range edits {
		total += len(fe.Edits)
	}
	if total == 0 {
		return "Rename produced no edits (the server declined, e.g. the position isn't a renameable symbol)."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Rename preview (%d edit(s) in %d file(s)) — NOT applied; use Edit to apply:\n", total, len(edits))
	for _, fe := range edits {
		fmt.Fprintf(&b, "%s:\n", lsp.URIPath(fe.URI))
		for _, e := range fe.Edits {
			fmt.Fprintf(&b, "  %d:%d", e.Range.Start.Line+1, e.Range.Start.Character+1)
			if src := lsp.SourceLine(fe.URI, e.Range.Start.Line); src != "" {
				fmt.Fprintf(&b, "  %s", strings.TrimSpace(src))
			}
			fmt.Fprintf(&b, "  →  %q\n", e.NewText)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
