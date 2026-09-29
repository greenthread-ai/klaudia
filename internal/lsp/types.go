// Package lsp is a minimal Language Server Protocol client: it detects language
// servers already installed on the machine (PATH plus well-known toolchain
// locations), spawns them on demand, and exposes diagnostics, definitions, and
// references to the agent's tools. It does not download servers.
package lsp

import (
	"net/url"
	"path/filepath"
)

// Position is a 0-based line/character offset in a document.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a span between two positions.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a range within a document URI.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Diagnostic is one problem reported by a server.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"` // 1=Error 2=Warning 3=Info 4=Hint
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Hover is a decoded textDocument/hover result: the server's info text and,
// when the server supplied one, the range the hover applies to.
type Hover struct {
	Text  string
	Range *Range
}

// Symbol is a normalised document symbol: LSP returns either a flat
// SymbolInformation[] or a hierarchical DocumentSymbol[]; both collapse to this.
type Symbol struct {
	Name      string
	Kind      int
	Detail    string   // e.g. a function signature (DocumentSymbol only)
	Location  Location // where the symbol lives
	Container string   // enclosing symbol name (parent, or SymbolInformation.containerName)
}

// TextEdit is a single edit: replace Range with NewText.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// FileEdits groups the edits a rename makes to one file. Rename results are
// returned as a preview — the caller decides whether to apply them.
type FileEdits struct {
	URI   string
	Edits []TextEdit
}

// SymbolKindName maps an LSP SymbolKind to a short label (spec §Symbol Kind).
func SymbolKindName(kind int) string {
	switch kind {
	case 1:
		return "file"
	case 2:
		return "module"
	case 3:
		return "namespace"
	case 4:
		return "package"
	case 5:
		return "class"
	case 6:
		return "method"
	case 7:
		return "property"
	case 8:
		return "field"
	case 9:
		return "constructor"
	case 10:
		return "enum"
	case 11:
		return "interface"
	case 12:
		return "function"
	case 13:
		return "variable"
	case 14:
		return "constant"
	case 15:
		return "string"
	case 16:
		return "number"
	case 17:
		return "boolean"
	case 18:
		return "array"
	case 19:
		return "object"
	case 20:
		return "key"
	case 21:
		return "null"
	case 22:
		return "enum-member"
	case 23:
		return "struct"
	case 24:
		return "event"
	case 25:
		return "operator"
	case 26:
		return "type-parameter"
	default:
		return "symbol"
	}
}

// SeverityName maps an LSP severity to a short label.
func SeverityName(sev int) string {
	switch sev {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "diagnostic"
	}
}

// pathToURI converts an absolute file path to a file:// URI.
func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

// URIPath converts a file:// URI back to a local path for display (best effort).
func URIPath(uri string) string { return uriToPath(uri) }

// uriToPath converts a file:// URI back to a local path (best effort).
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return filepath.FromSlash(u.Path)
}
