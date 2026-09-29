package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestURIRoundTrip(t *testing.T) {
	abs := "/tmp/some dir/main.go" // includes a space → must be escaped
	uri := pathToURI(abs)
	if !strings.HasPrefix(uri, "file://") {
		t.Errorf("uri = %q, want a file:// URI", uri)
	}
	if got := uriToPath(uri); got != abs {
		t.Errorf("round-trip = %q, want %q", got, abs)
	}
}

func TestParseLocations(t *testing.T) {
	// Array form.
	arr := json.RawMessage(`[{"uri":"file:///a.go","range":{"start":{"line":2,"character":1},"end":{"line":2,"character":4}}}]`)
	if got := parseLocations(arr); len(got) != 1 || got[0].Range.Start.Line != 2 {
		t.Errorf("array parse = %+v", got)
	}
	// Single object form.
	one := json.RawMessage(`{"uri":"file:///b.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}`)
	if got := parseLocations(one); len(got) != 1 || got[0].URI != "file:///b.go" {
		t.Errorf("single parse = %+v", got)
	}
	// null / empty.
	if parseLocations(json.RawMessage("null")) != nil || parseLocations(nil) != nil {
		t.Error("null/empty should parse to nil")
	}
}

func TestSeverityName(t *testing.T) {
	if SeverityName(1) != "error" || SeverityName(2) != "warning" || SeverityName(99) != "diagnostic" {
		t.Error("severity names wrong")
	}
}

func TestSymbolKindName(t *testing.T) {
	if SymbolKindName(12) != "function" || SymbolKindName(23) != "struct" || SymbolKindName(999) != "symbol" {
		t.Errorf("symbol kind names wrong: %q %q %q",
			SymbolKindName(12), SymbolKindName(23), SymbolKindName(999))
	}
}

func TestCapEnabled(t *testing.T) {
	cases := map[string]bool{
		"true":                      true,
		"false":                     false,
		"null":                      false,
		"":                          false,
		`{"workDoneProgress":true}`: true,
		`{}`:                        true,
	}
	for raw, want := range cases {
		if got := capEnabled(json.RawMessage(raw)); got != want {
			t.Errorf("capEnabled(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestParseHover(t *testing.T) {
	// MarkupContent {kind,value}.
	markup := json.RawMessage(`{"contents":{"kind":"markdown","value":"func Foo()"},"range":{"start":{"line":3,"character":5},"end":{"line":3,"character":8}}}`)
	h := parseHover(markup)
	if h == nil || h.Text != "func Foo()" || h.Range == nil || h.Range.Start.Line != 3 {
		t.Errorf("markup hover = %+v", h)
	}
	// Plain string contents.
	if h := parseHover(json.RawMessage(`{"contents":"plain text"}`)); h == nil || h.Text != "plain text" {
		t.Errorf("string hover = %+v", h)
	}
	// Array of MarkedString (string + {language,value}).
	arr := json.RawMessage(`{"contents":["one",{"language":"go","value":"two"}]}`)
	if h := parseHover(arr); h == nil || h.Text != "one\ntwo" {
		t.Errorf("array hover = %+v", h)
	}
	// null / empty content → nil.
	if parseHover(json.RawMessage("null")) != nil || parseHover(json.RawMessage(`{"contents":""}`)) != nil {
		t.Error("empty hover should be nil")
	}
}

func TestParseSymbolsHierarchical(t *testing.T) {
	// DocumentSymbol[] with a nested child; selectionRange is the reported loc.
	raw := json.RawMessage(`[{
		"name":"Server","kind":23,
		"range":{"start":{"line":10,"character":0},"end":{"line":40,"character":1}},
		"selectionRange":{"start":{"line":10,"character":5},"end":{"line":10,"character":11}},
		"children":[{
			"name":"Serve","detail":"func()","kind":6,
			"range":{"start":{"line":20,"character":1},"end":{"line":25,"character":2}},
			"selectionRange":{"start":{"line":20,"character":8},"end":{"line":20,"character":13}}
		}]
	}]`)
	syms := parseSymbols(raw, "file:///s.go")
	if len(syms) != 2 {
		t.Fatalf("want 2 symbols, got %d: %+v", len(syms), syms)
	}
	if syms[0].Name != "Server" || syms[0].Kind != 23 || syms[0].Location.Range.Start.Line != 10 {
		t.Errorf("parent = %+v", syms[0])
	}
	if syms[1].Name != "Serve" || syms[1].Container != "Server" || syms[1].Location.Range.Start.Line != 20 || syms[1].Detail != "func()" {
		t.Errorf("child = %+v", syms[1])
	}
}

func TestParseSymbolsFlat(t *testing.T) {
	// SymbolInformation[] (no selectionRange; carries a Location + containerName).
	raw := json.RawMessage(`[{
		"name":"Foo","kind":12,"containerName":"pkg",
		"location":{"uri":"file:///a.go","range":{"start":{"line":2,"character":5},"end":{"line":2,"character":8}}}
	}]`)
	syms := parseSymbols(raw, "file:///ignored.go")
	if len(syms) != 1 || syms[0].Name != "Foo" || syms[0].Container != "pkg" || syms[0].Location.URI != "file:///a.go" {
		t.Errorf("flat symbols = %+v", syms)
	}
	if parseSymbols(json.RawMessage("null"), "") != nil {
		t.Error("null symbols should be nil")
	}
}

func TestParseWorkspaceEdit(t *testing.T) {
	// `changes` map form.
	changes := json.RawMessage(`{"changes":{"file:///a.go":[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":3}},"newText":"Bar"}]}}`)
	got := parseWorkspaceEdit(changes)
	if len(got) != 1 || got[0].URI != "file:///a.go" || len(got[0].Edits) != 1 || got[0].Edits[0].NewText != "Bar" {
		t.Errorf("changes form = %+v", got)
	}
	// `documentChanges` array form.
	docChanges := json.RawMessage(`{"documentChanges":[{"textDocument":{"uri":"file:///b.go","version":1},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"X"}]}]}`)
	got = parseWorkspaceEdit(docChanges)
	if len(got) != 1 || got[0].URI != "file:///b.go" || got[0].Edits[0].NewText != "X" {
		t.Errorf("documentChanges form = %+v", got)
	}
	if parseWorkspaceEdit(json.RawMessage("null")) != nil {
		t.Error("null workspace edit should be nil")
	}
}

func TestSourceLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src.go")
	write(t, path, "package main\n\nfunc main() {}\n")
	// 0-based line 2 is the func line; trailing content is trimmed.
	if got := SourceLine(pathToURI(path), 2); got != "func main() {}" {
		t.Errorf("SourceLine = %q", got)
	}
	// Out-of-range line and missing file → empty, not a panic.
	if SourceLine(pathToURI(path), 99) != "" || SourceLine(pathToURI(filepath.Join(dir, "nope.go")), 0) != "" {
		t.Error("SourceLine should be empty for missing line/file")
	}
	// A bare local path (not a file:// URI) also works.
	if got := SourceLine(path, 0); got != "package main" {
		t.Errorf("SourceLine(plain path) = %q", got)
	}
}

func TestDetectFindsInCandidateDir(t *testing.T) {
	dir := t.TempDir()
	// A fake executable not on PATH, found via the spec's candidate dirs.
	bin := filepath.Join(dir, "fakelsp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := ServerSpec{Bin: "fakelsp", candidates: func() []string { return []string{dir} }}
	got, ok := detect(spec)
	if !ok || got != bin {
		t.Errorf("detect = %q, %v; want %q", got, ok, bin)
	}
	// Unknown binary → not found.
	if _, ok := detect(ServerSpec{Bin: "definitely-not-installed-xyz"}); ok {
		t.Error("unexpectedly found a nonexistent server")
	}
}

// TestGoplsDiagnosticsLive exercises the whole pipeline against a real gopls.
// Skipped when gopls isn't installed.
func TestGoplsDiagnosticsLive(t *testing.T) {
	goSpec, ok := func() (ServerSpec, bool) {
		for _, s := range builtinServers {
			if s.Language == "go" {
				return s, true
			}
		}
		return ServerSpec{}, false
	}()
	if !ok {
		t.Skip("no go spec")
	}
	if _, found := detect(goSpec); !found {
		t.Skip("gopls not installed; skipping live LSP test")
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.com/m\n\ngo 1.21\n")
	// `x` is declared and not used → gopls reports an error.
	write(t, filepath.Join(dir, "main.go"), "package main\n\nfunc main() {\n\tx := 1\n}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := NewPool(ctx, dir, nil, nil)
	defer pool.Close()

	diags, err := pool.Diagnostics(ctx, filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic for an unused variable")
	}
	var found bool
	for _, d := range diags {
		if strings.Contains(strings.ToLower(d.Message), "not used") || strings.Contains(strings.ToLower(d.Message), "declared") {
			found = true
		}
	}
	if !found {
		t.Errorf("diagnostics didn't mention the unused var: %+v", diags)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
