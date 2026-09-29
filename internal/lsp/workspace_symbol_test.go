package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeServerEnv, when set, turns the test binary into a minimal language
// server (see TestMain). Its value picks how workspace/symbol answers.
const fakeServerEnv = "KLAUDIA_FAKE_LSP"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeServerEnv); mode != "" {
		runFakeServer(mode, os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runFakeServer speaks just enough LSP for the pool: initialize, shutdown,
// exit, and workspace/symbol. In "symbols" mode a query Q is answered with a
// SymbolInformation named Q, a WorkspaceSymbol (URI only, no range) named Q2,
// and an entry without a location that the client must drop. In "fail" mode
// workspace/symbol returns a JSON-RPC error.
func runFakeServer(mode string, in io.Reader, out io.Writer) {
	br := bufio.NewReader(in)
	tp := textproto.NewReader(br)
	send := func(v any) {
		body, _ := json.Marshal(v)
		fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	for {
		hdr, err := tp.ReadMIMEHeader()
		if err != nil {
			return
		}
		n, _ := strconv.Atoi(hdr.Get("Content-Length"))
		body := make([]byte, n)
		if _, err := io.ReadFull(br, body); err != nil {
			return
		}
		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		if msg.Method == "exit" {
			return
		}
		if msg.ID == nil {
			continue // notification
		}
		reply := map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": nil}
		switch msg.Method {
		case "initialize":
			reply["result"] = map[string]any{"capabilities": map[string]any{"workspaceSymbolProvider": true}}
		case "workspace/symbol":
			var p struct {
				Query string `json:"query"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if mode == "fail" {
				delete(reply, "result")
				reply["error"] = map[string]any{"code": -32603, "message": "index not ready"}
				break
			}
			reply["result"] = []map[string]any{
				{"name": p.Query, "kind": 12, "containerName": "pkg", "location": map[string]any{
					"uri":   "file:///w/a.go",
					"range": map[string]any{"start": map[string]any{"line": 4, "character": 5}, "end": map[string]any{"line": 4, "character": 8}},
				}},
				{"name": p.Query + "2", "kind": 23, "location": map[string]any{"uri": "file:///w/b.go"}},
				{"name": "nowhere", "kind": 13},
			}
		}
		send(reply)
	}
}

// fakeSpec points a language at the test binary acting as a fake server.
func fakeSpec(t *testing.T) ServerSpec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return ServerSpec{Bin: exe}
}

func TestParseSymbols(t *testing.T) {
	raw := json.RawMessage(`[
		{"name":"NewPool","kind":12,"containerName":"lsp","location":{"uri":"file:///a.go","range":{"start":{"line":3,"character":5},"end":{"line":3,"character":12}}}},
		{"name":"Pool","kind":23,"location":{"uri":"file:///b.go"}},
		{"name":"orphan","kind":13}
	]`)
	got := parseSymbols(raw)
	if len(got) != 2 {
		t.Fatalf("parseSymbols = %+v, want 2 entries (the one without a URI dropped)", got)
	}
	if got[0].Name != "NewPool" || got[0].ContainerName != "lsp" || got[0].Location.Range.Start.Line != 3 {
		t.Errorf("SymbolInformation decoded wrong: %+v", got[0])
	}
	if got[1].Location.URI != "file:///b.go" || got[1].Location.Range.Start != (Position{}) {
		t.Errorf("URI-only WorkspaceSymbol decoded wrong: %+v", got[1])
	}
	if parseSymbols(json.RawMessage("null")) != nil || parseSymbols(nil) != nil || parseSymbols(json.RawMessage(`{"x":1}`)) != nil {
		t.Error("null, empty and malformed results should parse to nil")
	}
}

func TestSymbolKindName(t *testing.T) {
	if SymbolKindName(12) != "function" || SymbolKindName(23) != "struct" || SymbolKindName(26) != "type parameter" {
		t.Error("symbol kind names wrong")
	}
	if SymbolKindName(0) != "symbol" || SymbolKindName(99) != "symbol" {
		t.Error("unknown kinds should fall back to \"symbol\"")
	}
}

func TestWorkspaceSpecsFollowMarkersAndConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module m\n")
	write(t, filepath.Join(dir, "Cargo.toml"), "[package]\n")

	langs := func(p *Pool) []string {
		var out []string
		for _, s := range p.workspaceSpecs() {
			out = append(out, s.Language)
		}
		return out
	}
	if got := strings.Join(langs(NewPool(context.Background(), dir, nil, nil)), ","); got != "go,rust" {
		t.Errorf("workspace languages = %q, want go,rust", got)
	}
	if got := strings.Join(langs(NewPool(context.Background(), dir, []string{"rust"}, nil)), ","); got != "go" {
		t.Errorf("with rust disabled = %q, want go", got)
	}
	if got := langs(NewPool(context.Background(), t.TempDir(), nil, nil)); len(got) != 0 {
		t.Errorf("empty root = %v, want none", got)
	}
}

func TestWorkspaceSymbolFakeServer(t *testing.T) {
	t.Setenv(fakeServerEnv, "symbols")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module m\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := NewPool(ctx, dir, nil, map[string]ServerSpec{"go": fakeSpec(t)})
	defer pool.Close()

	// Without a file: go.mod at the root selects the Go server.
	syms, err := pool.WorkspaceSymbol(ctx, "Frob", "")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	if len(syms) != 2 || syms[0].Name != "Frob" || syms[1].Name != "Frob2" {
		t.Fatalf("symbols = %+v, want Frob and Frob2", syms)
	}
	if syms[0].Kind != 12 || syms[0].ContainerName != "pkg" || syms[0].Location.Range.Start.Line != 4 {
		t.Errorf("first symbol decoded wrong: %+v", syms[0])
	}

	// With a file: that file's language answers, reusing the running server.
	syms, err = pool.WorkspaceSymbol(ctx, "Other", filepath.Join(dir, "x.go"))
	if err != nil || len(syms) != 2 || syms[0].Name != "Other" {
		t.Fatalf("with file: %+v, %v", syms, err)
	}
	if n := len(pool.clients); n != 1 {
		t.Errorf("pool spawned %d servers, want 1 reused", n)
	}

	// A file in a language with no server is an error, not a silent empty.
	if _, err := pool.WorkspaceSymbol(ctx, "x", filepath.Join(dir, "notes.txt")); err == nil {
		t.Error("expected an error for a file no server handles")
	}
}

func TestWorkspaceSymbolPartialFailure(t *testing.T) {
	t.Setenv(fakeServerEnv, "symbols")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module m\n")
	write(t, filepath.Join(dir, "Cargo.toml"), "[package]\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := NewPool(ctx, dir, nil, map[string]ServerSpec{
		"go":   fakeSpec(t),
		"rust": {Bin: "definitely-not-installed-xyz"},
	})
	defer pool.Close()

	syms, err := pool.WorkspaceSymbol(ctx, "Frob", "")
	if len(syms) != 2 {
		t.Errorf("Go matches should survive the Rust failure, got %+v", syms)
	}
	if err == nil || !strings.Contains(err.Error(), "rust") {
		t.Errorf("err = %v, want one naming the rust server", err)
	}
}

func TestWorkspaceSymbolServerError(t *testing.T) {
	t.Setenv(fakeServerEnv, "fail")
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module m\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := NewPool(ctx, dir, nil, map[string]ServerSpec{"go": fakeSpec(t)})
	defer pool.Close()

	syms, err := pool.WorkspaceSymbol(ctx, "Frob", "")
	if len(syms) != 0 || err == nil || !strings.Contains(err.Error(), "index not ready") {
		t.Errorf("got %+v, %v; want the server's error", syms, err)
	}
}

func TestWorkspaceSymbolNoLanguage(t *testing.T) {
	pool := NewPool(context.Background(), t.TempDir(), nil, nil)
	defer pool.Close()
	if _, err := pool.WorkspaceSymbol(context.Background(), "x", ""); err == nil || !strings.Contains(err.Error(), "pass a file") {
		t.Errorf("err = %v, want a hint to pass a file", err)
	}
}
