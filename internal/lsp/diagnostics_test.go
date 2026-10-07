package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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

// fakeDiagServerEnv, when set, makes TestFakeDiagnosticsServer act as a
// minimal language server on stdin/stdout instead of a test. Its value picks
// what the server does when a document is opened:
//
//	silent — never publishes diagnostics (a cold server still indexing)
//	clean  — publishes an empty diagnostics list
//	error  — publishes one error diagnostic
const fakeDiagServerEnv = "KLAUDIA_FAKE_DIAG_LSP"

// TestFakeDiagnosticsServer is not a test: it is the entry point the fake
// server runs through when the test binary re-executes itself (the os/exec
// helper-process pattern). Run normally, it returns at once.
func TestFakeDiagnosticsServer(t *testing.T) {
	mode := os.Getenv(fakeDiagServerEnv)
	if mode == "" {
		return
	}
	serveFakeDiagnostics(mode, os.Stdin, os.Stdout)
	os.Exit(0)
}

func serveFakeDiagnostics(mode string, in io.Reader, out io.Writer) {
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
		switch {
		case msg.Method == "exit":
			return
		case msg.Method == "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			diags := []map[string]any{}
			switch mode {
			case "silent":
				continue
			case "error":
				diags = append(diags, map[string]any{
					"range": map[string]any{
						"start": map[string]any{"line": 3, "character": 1},
						"end":   map[string]any{"line": 3, "character": 2},
					},
					"severity": 1, "message": "declared and not used: x", "source": "fake",
				})
			}
			send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
				"params": map[string]any{"uri": p.TextDocument.URI, "diagnostics": diags}})
		case msg.ID != nil:
			// initialize, shutdown: an empty success is enough for the client.
			send(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": map[string]any{}})
		}
	}
}

// fakeDiagPool returns a pool whose Go server is this test binary running
// TestFakeDiagnosticsServer in the given mode, plus a Go file to ask about.
func fakeDiagPool(t *testing.T, mode string) (context.Context, *Pool, string) {
	t.Helper()
	t.Setenv(fakeDiagServerEnv, mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	write(t, file, "package main\n\nfunc main() {\n\tx := 1\n}\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	spec := ServerSpec{Bin: exe, Args: []string{"-test.run=^TestFakeDiagnosticsServer$"}}
	pool := NewPool(ctx, dir, nil, map[string]ServerSpec{"go": spec})
	t.Cleanup(pool.Close)
	return ctx, pool, file
}

// shortDiagnosticsWait shrinks the publish wait so a silent server costs the
// test a fraction of a second rather than the production ten.
func shortDiagnosticsWait(t *testing.T) {
	t.Helper()
	old := diagnosticsWait
	diagnosticsWait = 300 * time.Millisecond
	t.Cleanup(func() { diagnosticsWait = old })
}

// A server that never publishes must not read as "no problems": that is a
// false all-clear on every cold gopls / rust-analyzer start (#109).
func TestDiagnosticsServerNeverReports(t *testing.T) {
	shortDiagnosticsWait(t)
	ctx, pool, file := fakeDiagPool(t, "silent")

	diags, err := pool.Diagnostics(ctx, file)
	if !errors.Is(err, ErrNoDiagnosticsReport) {
		t.Fatalf("Diagnostics = %+v, %v; want ErrNoDiagnosticsReport", diags, err)
	}
	if diags != nil {
		t.Errorf("diagnostics = %+v, want none alongside the error", diags)
	}
	if msg := err.Error(); !strings.Contains(msg, "main.go") || !strings.Contains(msg, "not an all-clear") {
		t.Errorf("error %q should name the file and say it is not an all-clear", msg)
	}
	// The abandoned waiter is unregistered rather than left for a publish
	// that may never come.
	c := pool.clients["go"]
	c.mu.Lock()
	left := len(c.diagCh)
	c.mu.Unlock()
	if left != 0 {
		t.Errorf("%d diagnostics waiter(s) left registered after the timeout", left)
	}
}

// A server that reports an empty list is the genuine "clean" answer.
func TestDiagnosticsServerReportsClean(t *testing.T) {
	shortDiagnosticsWait(t)
	ctx, pool, file := fakeDiagPool(t, "clean")

	diags, err := pool.Diagnostics(ctx, file)
	if err != nil || len(diags) != 0 {
		t.Fatalf("Diagnostics = %+v, %v; want an empty, successful report", diags, err)
	}
}

func TestDiagnosticsServerReportsError(t *testing.T) {
	shortDiagnosticsWait(t)
	ctx, pool, file := fakeDiagPool(t, "error")

	diags, err := pool.Diagnostics(ctx, file)
	if err != nil {
		t.Fatalf("Diagnostics: %v", err)
	}
	if len(diags) != 1 || diags[0].Severity != 1 || diags[0].Range.Start.Line != 3 {
		t.Errorf("diagnostics = %+v, want the one published error", diags)
	}
}
