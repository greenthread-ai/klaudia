package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"strconv"
	"testing"
	"time"
)

// The tests re-exec their own binary as a scripted language server, so the
// client, the pool and their failure paths run against a real child process
// speaking real Content-Length framing — without depending on gopls or any
// other server being installed.
//
// fakeLSPEnv selects the script; fakeLSPLogEnv names a file the server appends
// one line per event to ("start", "initialized", method names), which is how a
// test observes what the client sent.
const (
	fakeLSPEnv    = "KLAUDIA_FAKE_LSP"
	fakeLSPLogEnv = "KLAUDIA_FAKE_LSP_LOG"
)

func TestMain(m *testing.M) {
	if script := os.Getenv(fakeLSPEnv); script != "" {
		os.Exit(runFakeLSP(script, os.Stdin, os.Stdout))
	}
	os.Exit(m.Run())
}

// useFakeLSP arranges for any server the test spawns to run script, and
// returns a func reading the event log back.
func useFakeLSP(t *testing.T, script string) (bin string, events func() []string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	log := t.TempDir() + "/events.log"
	t.Setenv(fakeLSPEnv, script)
	t.Setenv(fakeLSPLogEnv, log)
	return exe, func() []string {
		data, _ := os.ReadFile(log)
		var out []string
		for _, l := range splitLines(string(data)) {
			if l != "" {
				out = append(out, l)
			}
		}
		return out
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

type fakeConn struct {
	br  *bufio.Reader
	tp  *textproto.Reader
	w   io.Writer
	log *os.File
}

func (c *fakeConn) event(s string) {
	if c.log != nil {
		fmt.Fprintln(c.log, s)
	}
}

func (c *fakeConn) read() (wireMessage, error) {
	hdr, err := c.tp.ReadMIMEHeader()
	if err != nil {
		return wireMessage{}, err
	}
	n, _ := strconv.Atoi(hdr.Get("Content-Length"))
	body := make([]byte, n)
	if _, err := io.ReadFull(c.br, body); err != nil {
		return wireMessage{}, err
	}
	var msg wireMessage
	err = json.Unmarshal(body, &msg)
	return msg, err
}

func (c *fakeConn) sendRaw(body string) {
	fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

func (c *fakeConn) send(v any) {
	b, _ := json.Marshal(v)
	c.sendRaw(string(b))
}

func (c *fakeConn) result(id *int, result any) {
	c.send(map[string]any{"jsonrpc": "2.0", "id": *id, "result": result})
}

func (c *fakeConn) fail(id *int, msg string) {
	c.send(map[string]any{"jsonrpc": "2.0", "id": *id, "error": map[string]any{"code": -32603, "message": msg}})
}

func loc(uri string, line int) map[string]any {
	p := map[string]any{"line": line, "character": 2}
	return map[string]any{"uri": uri, "range": map[string]any{"start": p, "end": p}}
}

// runFakeLSP serves one scripted session. Scripts:
//
//	ok             a well-behaved server (see the cases below)
//	init-error     answers initialize with a JSON-RPC error
//	crash-on-def   exits without replying when asked for a definition
//	mute           answers the handshake, then never replies or publishes
func runFakeLSP(script string, in io.Reader, out io.Writer) int {
	br := bufio.NewReader(in)
	c := &fakeConn{br: br, tp: textproto.NewReader(br), w: out}
	if p := os.Getenv(fakeLSPLogEnv); p != "" {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			c.log = f
			defer f.Close()
		}
	}
	c.event("start")
	for {
		msg, err := c.read()
		if err != nil {
			return 0
		}
		switch msg.Method {
		case "initialize":
			if script == "init-error" {
				c.fail(msg.ID, "cannot index this workspace")
				continue
			}
			// Noise a real server can emit, which the client must survive: a
			// frame with no length, a body that is not JSON, and a request of
			// its own that the client does not implement.
			fmt.Fprint(out, "Content-Length: 0\r\n\r\n")
			c.sendRaw("not json")
			c.send(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "workspace/configuration", "params": map[string]any{}})
			c.result(msg.ID, map[string]any{"capabilities": map[string]any{}})
		case "initialized":
			c.event("initialized")
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"textDocument"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			c.event(fmt.Sprintf("didOpen %q", p.TextDocument.Text))
			if script != "ok" {
				continue
			}
			c.send(map[string]any{
				"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
				"params": map[string]any{"uri": p.TextDocument.URI, "diagnostics": []any{
					map[string]any{
						"range":    map[string]any{"start": map[string]any{"line": 3, "character": 1}, "end": map[string]any{"line": 3, "character": 2}},
						"severity": 1, "source": "fake", "message": "declared and not used: x",
					},
				}},
			})
		case "textDocument/didClose":
			c.event("didClose")
		case "textDocument/definition":
			if script == "crash-on-def" {
				return 3
			}
			if script == "mute" {
				continue
			}
			var p struct {
				TextDocument struct{ URI string } `json:"textDocument"`
				Position     Position             `json:"position"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			// A single Location, not an array: servers may answer either way.
			c.result(msg.ID, loc(p.TextDocument.URI, p.Position.Line+10))
		case "textDocument/references":
			var p struct {
				TextDocument struct{ URI string } `json:"textDocument"`
				Context      struct {
					IncludeDeclaration bool `json:"includeDeclaration"`
				} `json:"context"`
			}
			_ = json.Unmarshal(msg.Params, &p)
			if !p.Context.IncludeDeclaration {
				c.fail(msg.ID, "includeDeclaration missing")
				continue
			}
			c.result(msg.ID, []any{loc(p.TextDocument.URI, 1), loc("file:///elsewhere.go", 7)})
		case "shutdown":
			c.event("shutdown")
			c.result(msg.ID, nil)
		case "exit":
			if script == "slow-exit" {
				// A server with work to flush: slower to act on exit than a
				// client that kills it immediately afterwards.
				time.Sleep(300 * time.Millisecond)
			}
			c.event("exit")
			return 0
		default:
			if msg.ID != nil && msg.Method == "" {
				// The client's reply to our workspace/configuration request.
				c.event(fmt.Sprintf("reply %d %s", *msg.ID, string(msg.Result)))
			}
		}
	}
}
