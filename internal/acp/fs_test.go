package acp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// replyError answers an inbound request with a JSON-RPC error.
func (p *peer) replyError(id json.RawMessage, e *rpcError) {
	p.t.Helper()
	b, err := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: id, Error: e})
	if err != nil {
		p.t.Fatal(err)
	}
	if _, err := p.in.Write(append(b, '\n')); err != nil {
		p.t.Fatal(err)
	}
}

// fsProbe runs one turn whose only job is to call turn.ReadText, and returns
// what the hook answered.
//
// serve, when non-nil, is called with the fs/read_text_file the hook provoked
// and decides the response; nil means the test expects the hook never to reach
// the wire, and fsProbe fails if it does.
type fsProbe struct {
	caps  clientCapabilities
	path  string
	line  int
	limit int
	serve func(p *peer, id json.RawMessage, req readTextFileParams)
}

func (probe fsProbe) run(t *testing.T, cwd string) (string, error) {
	t.Helper()
	type outcome struct {
		text string
		err  error
	}
	got := make(chan outcome, 1)
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
			text, err := turn.ReadText(ctx, probe.path, probe.line, probe.limit)
			got <- outcome{text, err}
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	p.call("initialize", initializeParams{
		ProtocolVersion: protocolVersion, ClientCapabilities: probe.caps,
	})
	res := p.call("session/new", newSessionParams{CWD: cwd})
	var sess newSessionResult
	if err := json.Unmarshal(res.Result, &sess); err != nil {
		t.Fatal(err)
	}
	sid := sess.SessionID
	p.awaitUpdate("available_commands_update")
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("read it")},
	})
	if probe.serve != nil {
		call := p.nextCall()
		if call.Method != "fs/read_text_file" {
			t.Fatalf("the agent called %s, want fs/read_text_file", call.Method)
		}
		var req readTextFileParams
		if err := json.Unmarshal(call.Params, &req); err != nil {
			t.Fatal(err)
		}
		if req.SessionID != sid {
			t.Errorf("fs/read_text_file named session %q, want %q", req.SessionID, sid)
		}
		probe.serve(p, call.ID, req)
	}
	o := <-got
	// Drain the prompt's response, and with it any request the hook made that
	// the test did not expect — nextCall would otherwise never see it.
	_, _, calls := p.await(id)
	if probe.serve == nil {
		for _, c := range calls {
			if c.Method == "fs/read_text_file" {
				t.Error("the agent called fs/read_text_file when it should not have")
			}
		}
	}
	return o.text, o.err
}

func TestReadTextFileGoesThroughTheClient(t *testing.T) {
	// The point of taking fs/read_text_file up: the editor answers from the
	// buffer it has open, so a file the user has changed and not saved comes
	// back as they see it. Reading disk here is how the model ends up
	// explaining text that is no longer there — and how an Edit gets built on
	// an old_string the user has already replaced.
	cwd := t.TempDir()
	var asked readTextFileParams
	text, err := fsProbe{
		caps: clientCapabilities{FS: fsCapabilities{ReadTextFile: true}},
		path: filepath.Join(cwd, "b.go"), line: 1,
		serve: func(p *peer, id json.RawMessage, req readTextFileParams) {
			asked = req
			p.reply(id, readTextFileResult{Content: "unsaved buffer\n"})
		},
	}.run(t, cwd)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if text != "unsaved buffer\n" {
		t.Errorf("text = %q, want the client's answer", text)
	}
	// line 1 with no limit is "the whole file", which is what an absent line
	// and limit already mean. Sending limit 0 explicitly would be worse than
	// redundant: the schema's minimum is 1.
	if asked.Line != nil || asked.Limit != nil {
		t.Errorf("a whole-file read asked for line=%v limit=%v, want neither",
			asked.Line, asked.Limit)
	}
}

func TestReadTextFilePassesTheWindow(t *testing.T) {
	// Read's offset and limit have to reach the client, or an editor holding a
	// 20,000-line file sends all of it back for a 50-line window.
	cwd := t.TempDir()
	var asked readTextFileParams
	_, err := fsProbe{
		caps: clientCapabilities{FS: fsCapabilities{ReadTextFile: true}},
		path: filepath.Join(cwd, "b.go"), line: 120, limit: 50,
		serve: func(p *peer, id json.RawMessage, req readTextFileParams) {
			asked = req
			p.reply(id, readTextFileResult{Content: "x\n"})
		},
	}.run(t, cwd)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if asked.Line == nil || *asked.Line != 120 {
		t.Errorf("line = %v, want 120", asked.Line)
	}
	if asked.Limit == nil || *asked.Limit != 50 {
		t.Errorf("limit = %v, want 50", asked.Limit)
	}
}

func TestReadTextFileIsNotAttemptedWithoutTheCapability(t *testing.T) {
	// Calling a method the client never advertised buys a method-not-found
	// round trip on every single Read. The hook refuses locally instead, and
	// the Read tool falls back to disk.
	cwd := t.TempDir()
	_, err := fsProbe{
		path: filepath.Join(cwd, "b.go"), line: 1,
	}.run(t, cwd)
	if !errors.Is(err, errNoFSRead) {
		t.Errorf("err = %v, want errNoFSRead", err)
	}
}

func TestReadTextFileRefusesARelativePath(t *testing.T) {
	// The spec requires an absolute path, and Read has already resolved
	// relative ones against the working dir — so anything still relative here
	// is a bug. Falling back to disk beats guessing what the editor would
	// resolve it against.
	cwd := t.TempDir()
	_, err := fsProbe{
		caps: clientCapabilities{FS: fsCapabilities{ReadTextFile: true}},
		path: "b.go", line: 1,
	}.run(t, cwd)
	if !errors.Is(err, errNoFSRead) {
		t.Errorf("err = %v, want errNoFSRead", err)
	}
}

func TestReadTextFileSurfacesAClientRefusal(t *testing.T) {
	// A client may advertise the capability and still refuse a given file —
	// one outside the workspace it has open, say. The error has to come back so
	// Read falls through to disk rather than handing the model an empty file.
	cwd := t.TempDir()
	text, err := fsProbe{
		caps: clientCapabilities{FS: fsCapabilities{ReadTextFile: true}},
		path: filepath.Join(cwd, "b.go"), line: 1,
		serve: func(p *peer, id json.RawMessage, _ readTextFileParams) {
			p.replyError(id, errorf(codeInvalidParams, "outside the workspace"))
		},
	}.run(t, cwd)
	if err == nil {
		t.Errorf("a refused read returned %q and no error", text)
	}
}
