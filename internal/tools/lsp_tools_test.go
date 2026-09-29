package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/lsp"
)

// TestFormatLocationsIncludesSourceLine verifies location results carry the
// actual source line text next to their file:line:col (issue #157).
func TestFormatLocationsIncludesSourceLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc Target() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := path // SourceLine/URIPath accept a plain path as well as a file:// URI
	locs := []lsp.Location{{
		URI:   uri,
		Range: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}},
	}}
	out := formatLocations(locs)
	if !strings.Contains(out, ":3:6") {
		t.Errorf("expected 1-based file:line:col in %q", out)
	}
	if !strings.Contains(out, "func Target() {}") {
		t.Errorf("expected source line text in %q", out)
	}
}

func TestFormatRenamePreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("var Old int\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := path // SourceLine/URIPath accept a plain path as well as a file:// URI
	edits := []lsp.FileEdits{{
		URI: uri,
		Edits: []lsp.TextEdit{{
			Range:   lsp.Range{Start: lsp.Position{Line: 0, Character: 4}, End: lsp.Position{Line: 0, Character: 7}},
			NewText: "New",
		}},
	}}
	out := formatRename("New", edits)
	if !strings.Contains(out, "NOT applied") {
		t.Errorf("rename preview should say edits are not applied: %q", out)
	}
	if !strings.Contains(out, "1:5") || !strings.Contains(out, "var Old int") || !strings.Contains(out, `"New"`) {
		t.Errorf("rename preview missing location/source/newText: %q", out)
	}

	// No edits → a clear message, still not an error.
	if got := formatRename("New", nil); !strings.Contains(got, "no edits") {
		t.Errorf("empty rename = %q", got)
	}
}
