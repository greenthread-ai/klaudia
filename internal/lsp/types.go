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
