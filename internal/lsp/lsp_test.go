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
