package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"
)

const (
	initializeTimeout = 30 * time.Second // servers like gopls index on init
	requestTimeout    = 15 * time.Second
	diagnosticsWait   = 10 * time.Second // wait for the server to push diagnostics
)

// Initialize performs the LSP handshake for a workspace root.
func (c *Client) Initialize(ctx context.Context, root string) error {
	ctx, cancel := context.WithTimeout(ctx, initializeTimeout)
	defer cancel()
	rootURI := pathToURI(root)
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"workspaceFolders": []map[string]any{
			{"uri": rootURI, "name": "workspace"},
		},
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"synchronization":    map[string]any{"didSave": true},
				"publishDiagnostics": map[string]any{},
				"definition":         map[string]any{},
				"references":         map[string]any{},
				"hover":              map[string]any{},
				"documentSymbol":     map[string]any{"hierarchicalDocumentSymbolSupport": true},
				"rename":             map[string]any{},
				"implementation":     map[string]any{},
			},
		},
	}
	var res struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := c.call(ctx, "initialize", params, &res); err != nil {
		return err
	}
	// Record which capabilities the server advertised, so requests for an
	// unsupported one fail fast with ErrNotSupported instead of hitting a
	// method-not-found error or, worse, hanging on a server that ignores it.
	caps := map[string]bool{}
	for name, raw := range res.Capabilities {
		caps[name] = capEnabled(raw)
	}
	c.mu.Lock()
	c.caps = caps
	c.mu.Unlock()
	return c.notify("initialized", map[string]any{})
}

// capEnabled reports whether a *Provider capability value means "supported".
// Servers report these as either a bool or an options object; only an explicit
// false (or a missing/null value) means unsupported.
func capEnabled(raw json.RawMessage) bool {
	s := string(raw)
	if len(raw) == 0 || s == "null" || s == "false" {
		return false
	}
	return true
}

// didOpen opens a file with its current on-disk contents so the server analyses
// it (the agent's edits hit disk before this runs).
func (c *Client) didOpen(path, languageID string) (uri string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	uri = pathToURI(path)
	return uri, c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": languageID, "version": 1, "text": string(data),
		},
	})
}

func (c *Client) didClose(uri string) {
	_ = c.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// Diagnostics opens path, waits for the server to publish diagnostics for it
// (or a short timeout), and returns them. An empty slice means "no problems".
func (c *Client) Diagnostics(ctx context.Context, path, languageID string) ([]Diagnostic, error) {
	uri := pathToURI(path)
	// Register the waiter BEFORE opening, so a fast server push isn't missed
	// (which would otherwise force a full-timeout wait).
	c.mu.Lock()
	wait := make(chan struct{})
	c.diagCh[uri] = append(c.diagCh[uri], wait)
	c.mu.Unlock()

	if _, err := c.didOpen(path, languageID); err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	select {
	case <-wait:
	case <-time.After(diagnosticsWait):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.diags[uri], nil
}

// Definition returns the definition location(s) for the symbol at pos.
func (c *Client) Definition(ctx context.Context, path, languageID string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/definition", "definitionProvider", path, languageID, pos, nil)
}

// References returns all references to the symbol at pos (including its declaration).
func (c *Client) References(ctx context.Context, path, languageID string, pos Position) ([]Location, error) {
	extra := map[string]any{"context": map[string]any{"includeDeclaration": true}}
	return c.locations(ctx, "textDocument/references", "referencesProvider", path, languageID, pos, extra)
}

// Implementation returns the implementation location(s) for the symbol at pos
// (e.g. the concrete types implementing an interface method).
func (c *Client) Implementation(ctx context.Context, path, languageID string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/implementation", "implementationProvider", path, languageID, pos, nil)
}

func (c *Client) locations(ctx context.Context, method, capability, path, languageID string, pos Position, extra map[string]any) ([]Location, error) {
	if !c.supports(capability) {
		return nil, ErrNotSupported
	}
	uri, err := c.didOpen(path, languageID)
	if err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}
	for k, v := range extra {
		params[k] = v
	}
	// The result may be a single Location or an array; decode the raw result once.
	var raw json.RawMessage
	if err := c.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	return parseLocations(raw), nil
}

// parseLocations decodes an LSP location result, which is either a single
// Location, an array of Locations, or null.
func parseLocations(raw json.RawMessage) []Location {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var arr []Location
	if json.Unmarshal(raw, &arr) == nil {
		return arr
	}
	var one Location
	if json.Unmarshal(raw, &one) == nil && one.URI != "" {
		return []Location{one}
	}
	return nil
}

// Hover returns the hover information for the symbol at pos, or nil when the
// server has nothing to say there.
func (c *Client) Hover(ctx context.Context, path, languageID string, pos Position) (*Hover, error) {
	if !c.supports("hoverProvider") {
		return nil, ErrNotSupported
	}
	uri, err := c.didOpen(path, languageID)
	if err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}, &raw); err != nil {
		return nil, err
	}
	return parseHover(raw), nil
}

// DocumentSymbols returns the symbols declared in a file. LSP servers answer
// with either a flat SymbolInformation[] or a hierarchical DocumentSymbol[];
// both are flattened to a single ordered list.
func (c *Client) DocumentSymbols(ctx context.Context, path, languageID string) ([]Symbol, error) {
	if !c.supports("documentSymbolProvider") {
		return nil, ErrNotSupported
	}
	uri, err := c.didOpen(path, languageID)
	if err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/documentSymbol", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}, &raw); err != nil {
		return nil, err
	}
	return parseSymbols(raw, uri), nil
}

// Rename computes the workspace edit for renaming the symbol at pos to newName.
// It returns the edits grouped by file as a preview; it does NOT apply them —
// the caller decides. Returns nil edits when the server declines the rename.
func (c *Client) Rename(ctx context.Context, path, languageID string, pos Position, newName string) ([]FileEdits, error) {
	if !c.supports("renameProvider") {
		return nil, ErrNotSupported
	}
	uri, err := c.didOpen(path, languageID)
	if err != nil {
		return nil, err
	}
	defer c.didClose(uri)

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
		"newName":      newName,
	}, &raw); err != nil {
		return nil, err
	}
	return parseWorkspaceEdit(raw), nil
}

// parseHover decodes a textDocument/hover result. The `contents` field may be a
// MarkupContent object, a MarkedString (string or {language,value}), or an
// array of MarkedStrings; all collapse to plain text.
func parseHover(raw json.RawMessage) *Hover {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var h struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range"`
	}
	if json.Unmarshal(raw, &h) != nil {
		return nil
	}
	text := markupText(h.Contents)
	if text == "" {
		return nil
	}
	return &Hover{Text: text, Range: h.Range}
}

// markupText flattens the several shapes an LSP hover `contents` can take.
func markupText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	// Plain string.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	// MarkupContent {kind,value} or MarkedString {language,value}.
	var obj struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Value != "" {
		return strings.TrimSpace(obj.Value)
	}
	// Array of MarkedString.
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		var parts []string
		for _, el := range arr {
			if t := markupText(el); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// parseSymbols decodes a textDocument/documentSymbol result into a flat list.
// A DocumentSymbol[] carries selectionRange/range and children (hierarchical);
// a SymbolInformation[] carries a Location and containerName (flat).
func parseSymbols(raw json.RawMessage, uri string) []Symbol {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// Try hierarchical DocumentSymbol[] first (has selectionRange).
	var hierarchical []documentSymbol
	if json.Unmarshal(raw, &hierarchical) == nil && len(hierarchical) > 0 && hierarchical[0].SelectionRange != nil {
		var out []Symbol
		for i := range hierarchical {
			flattenSymbol(hierarchical[i], uri, "", &out)
		}
		return out
	}
	// Fall back to flat SymbolInformation[].
	var flat []struct {
		Name          string   `json:"name"`
		Kind          int      `json:"kind"`
		Location      Location `json:"location"`
		ContainerName string   `json:"containerName"`
	}
	if json.Unmarshal(raw, &flat) == nil {
		var out []Symbol
		for _, s := range flat {
			out = append(out, Symbol{Name: s.Name, Kind: s.Kind, Location: s.Location, Container: s.ContainerName})
		}
		return out
	}
	return nil
}

type documentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange *Range           `json:"selectionRange"`
	Children       []documentSymbol `json:"children"`
}

// flattenSymbol appends a DocumentSymbol and its descendants to out, using
// selectionRange (the name span) as the reported location.
func flattenSymbol(d documentSymbol, uri, container string, out *[]Symbol) {
	loc := Location{URI: uri, Range: d.Range}
	if d.SelectionRange != nil {
		loc.Range = *d.SelectionRange
	}
	*out = append(*out, Symbol{Name: d.Name, Kind: d.Kind, Detail: d.Detail, Location: loc, Container: container})
	for i := range d.Children {
		flattenSymbol(d.Children[i], uri, d.Name, out)
	}
}

// parseWorkspaceEdit decodes a WorkspaceEdit into per-file edit groups. It
// accepts both the `changes` map and the `documentChanges` array forms.
func parseWorkspaceEdit(raw json.RawMessage) []FileEdits {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var we struct {
		Changes         map[string][]TextEdit `json:"changes"`
		DocumentChanges []struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []TextEdit `json:"edits"`
		} `json:"documentChanges"`
	}
	if json.Unmarshal(raw, &we) != nil {
		return nil
	}
	var out []FileEdits
	for uri, edits := range we.Changes {
		out = append(out, FileEdits{URI: uri, Edits: edits})
	}
	for _, dc := range we.DocumentChanges {
		if dc.TextDocument.URI != "" {
			out = append(out, FileEdits{URI: dc.TextDocument.URI, Edits: dc.Edits})
		}
	}
	return out
}

// SourceLine returns the text of a 0-based line in the file named by a file://
// URI, trimmed of trailing whitespace. It returns "" when the file or line is
// unavailable, so callers can include it opportunistically.
func SourceLine(uri string, line int) string {
	path := uriToPath(uri)
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20) // tolerate long lines
	for i := 0; sc.Scan(); i++ {
		if i == line {
			return strings.TrimRight(sc.Text(), " \t\r\n")
		}
	}
	return ""
}
