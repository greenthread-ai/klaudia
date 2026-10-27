package acp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// peer is a test ACP client driving a real Agent over a pipe.
type peer struct {
	t    *testing.T
	in   *io.PipeWriter
	sink *frameSink
	done chan error

	nextID int
}

// serveAgent starts an Agent and returns a peer that talks to it. opts.CWD and
// opts.Run are defaulted when the test does not care.
func serveAgent(t *testing.T, opts Options) *peer {
	t.Helper()
	if opts.CWD == "" {
		opts.CWD = t.TempDir()
	}
	if opts.Run == nil {
		opts.Run = func(context.Context, agent.Turn) (agent.Result, error) {
			return agent.Result{Text: "done", StopReason: "end_turn"}, nil
		}
	}
	if opts.NewSessionID == nil {
		var n int
		var mu sync.Mutex
		opts.NewSessionID = func() string {
			mu.Lock()
			defer mu.Unlock()
			n++
			return "sess_" + strconv.Itoa(n)
		}
	}
	sink := newFrameSink()
	inR, inW := io.Pipe()
	a := New(opts, sink)
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- a.Serve(ctx, inR) }()
	t.Cleanup(func() {
		_ = inW.Close()
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Serve did not return after stdin closed")
		}
	})
	return &peer{t: t, in: inW, sink: sink, done: done}
}

// send writes a request and returns its id.
func (p *peer) send(method string, params any) string {
	p.t.Helper()
	p.nextID++
	id := strconv.Itoa(p.nextID)
	b, err := json.Marshal(rpcMessage{
		JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: mustJSON(p.t, params),
	})
	if err != nil {
		p.t.Fatal(err)
	}
	if _, err := p.in.Write(append(b, '\n')); err != nil {
		p.t.Fatal(err)
	}
	return id
}

func (p *peer) notify(method string, params any) {
	p.t.Helper()
	b, err := json.Marshal(rpcMessage{JSONRPC: "2.0", Method: method, Params: mustJSON(p.t, params)})
	if err != nil {
		p.t.Fatal(err)
	}
	if _, err := p.in.Write(append(b, '\n')); err != nil {
		p.t.Fatal(err)
	}
}

// reply answers an inbound request from the agent.
func (p *peer) reply(id json.RawMessage, result any) {
	p.t.Helper()
	b, err := json.Marshal(rpcMessage{JSONRPC: "2.0", ID: id, Result: mustJSON(p.t, result)})
	if err != nil {
		p.t.Fatal(err)
	}
	if _, err := p.in.Write(append(b, '\n')); err != nil {
		p.t.Fatal(err)
	}
}

// await returns the response to id, collecting any notifications and inbound
// requests that arrive first.
func (p *peer) await(id string) (rpcMessage, []sessionNotification, []rpcMessage) {
	p.t.Helper()
	var updates []sessionNotification
	var calls []rpcMessage
	for {
		m := p.sink.next(p.t)
		switch {
		case string(m.ID) == id && m.Method == "":
			return m, updates, calls
		case m.Method == "session/update":
			var n sessionNotification
			if err := json.Unmarshal(m.Params, &n); err != nil {
				p.t.Fatalf("undecodable session/update: %v", err)
			}
			updates = append(updates, n)
		case m.Method != "":
			calls = append(calls, m)
		}
	}
}

// call sends a request and returns its response, ignoring anything else.
func (p *peer) call(method string, params any) rpcMessage {
	p.t.Helper()
	res, _, _ := p.await(p.send(method, params))
	return res
}

// nextCall waits for the agent's next outbound request.
func (p *peer) nextCall() rpcMessage {
	p.t.Helper()
	for {
		m := p.sink.next(p.t)
		if m.Method != "" && m.Method != "session/update" {
			return m
		}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// newSession does initialize + session/new and returns the session id.
func (p *peer) newSession(cwd string) string {
	p.t.Helper()
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
	res := p.call("session/new", newSessionParams{CWD: cwd})
	if res.Error != nil {
		p.t.Fatalf("session/new: %+v", res.Error)
	}
	var out newSessionResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		p.t.Fatal(err)
	}
	// session/new's available_commands_update is sent *after* the response
	// (answer.after), so it is still in flight here. Drain it, or every test
	// that prompts afterwards finds it at the head of that turn's updates.
	p.awaitUpdate("available_commands_update")
	return out.SessionID
}

// awaitUpdate waits for the next session/update of the given kind, discarding
// updates of other kinds.
func (p *peer) awaitUpdate(kind string) sessionNotification {
	p.t.Helper()
	for {
		m := p.sink.next(p.t)
		if m.Method != "session/update" {
			continue
		}
		var n sessionNotification
		if err := json.Unmarshal(m.Params, &n); err != nil {
			p.t.Fatalf("undecodable session/update: %v", err)
		}
		if u, ok := n.Update.(map[string]any); ok && u["sessionUpdate"] == kind {
			return n
		}
	}
}

func TestInitialize(t *testing.T) {
	p := serveAgent(t, Options{})
	res := p.call("initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
	})
	if res.Error != nil {
		t.Fatalf("initialize: %+v", res.Error)
	}
	var out initializeResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.ProtocolVersion != 1 {
		t.Errorf("protocolVersion = %d, want 1", out.ProtocolVersion)
	}
	// embeddedContext yes, image/audio no: the agent loop takes a string
	// prompt, so claiming image support would have the editor send a
	// screenshot Klaudia silently discarded.
	if !out.AgentCapabilities.PromptCapabilities.EmbeddedContext {
		t.Error("embeddedContext should be advertised")
	}
	if out.AgentCapabilities.PromptCapabilities.Image || out.AgentCapabilities.PromptCapabilities.Audio {
		t.Error("image/audio must not be advertised while the loop takes a string prompt")
	}
	if out.AuthMethods == nil {
		// An absent array and an empty one read differently to a client
		// deciding whether it must authenticate.
		t.Error("authMethods should be an empty array, not null")
	}
}

func TestInitializeAnswersItsOwnVersionToANewerClient(t *testing.T) {
	// The spec: answer with the requested version when supported, otherwise
	// with the newest supported. Either way the client decides whether it can
	// go on — lying about speaking v2 would strand it.
	p := serveAgent(t, Options{})
	res := p.call("initialize", map[string]any{"protocolVersion": 99})
	var out initializeResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.ProtocolVersion != protocolVersion {
		t.Errorf("protocolVersion = %d, want %d", out.ProtocolVersion, protocolVersion)
	}
}

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	p := serveAgent(t, Options{})
	res := p.call("session/set_model", map[string]any{})
	if res.Error == nil || res.Error.Code != codeMethodNotFound {
		t.Errorf("error = %+v, want method not found", res.Error)
	}
}

func TestNewSession(t *testing.T) {
	cwd := t.TempDir()
	tests := []struct {
		name    string
		cwd     string
		wantErr bool
	}{
		{name: "the project root the process serves", cwd: cwd},
		{name: "a relative path is refused", cwd: "project", wantErr: true},
		{name: "a different root is refused", cwd: filepath.Join(cwd, "sub"), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := serveAgent(t, Options{CWD: cwd, Mode: permission.ModePlan})
			p.call("initialize", initializeParams{ProtocolVersion: 1})
			res := p.call("session/new", newSessionParams{CWD: tc.cwd})
			if tc.wantErr {
				if res.Error == nil {
					t.Fatalf("got result %s, want an error", res.Result)
				}
				if res.Error.Code != codeInvalidParams {
					t.Errorf("code = %d, want invalid params", res.Error.Code)
				}
				return
			}
			if res.Error != nil {
				t.Fatalf("session/new: %+v", res.Error)
			}
			var out newSessionResult
			if err := json.Unmarshal(res.Result, &out); err != nil {
				t.Fatal(err)
			}
			if out.SessionID == "" {
				t.Error("no session id")
			}
			if out.Modes == nil || out.Modes.CurrentModeID != string(permission.ModePlan) {
				t.Fatalf("modes = %+v, want the mode the process started in", out.Modes)
			}
			if len(out.Modes.AvailableModes) != len(permission.SelectableModes()) {
				t.Errorf("availableModes = %+v", out.Modes.AvailableModes)
			}
			for _, m := range out.Modes.AvailableModes {
				if m.ID == "" || m.Name == "" || m.Description == "" {
					t.Errorf("mode %+v is missing a field a picker needs", m)
				}
			}
		})
	}
}

func TestPromptRunsATurnAndStreamsIt(t *testing.T) {
	cwd := t.TempDir()
	var gotPrompt string
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			gotPrompt = turn.Prompt
			turn.Emit(agent.Event{Type: "assistant", Text: "I will "})
			turn.Emit(agent.Event{Type: "tool_use", ToolName: "Read", ToolUseID: "tu_1",
				Input: map[string]any{"file_path": "/a/b.go"}})
			turn.Emit(agent.Event{Type: "tool_result", ToolName: "Read", ToolUseID: "tu_1",
				Content: "package main"})
			turn.Emit(agent.Event{Type: "assistant", Text: "read it."})
			return agent.Result{Text: "I will read it.", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)

	id := p.send("session/prompt", promptParams{
		SessionID: sid,
		Prompt:    []contentBlock{textBlock("what is in b.go?")},
	})
	res, updates, _ := p.await(id)
	if res.Error != nil {
		t.Fatalf("session/prompt: %+v", res.Error)
	}
	var out promptResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.StopReason != stopEndTurn {
		t.Errorf("stopReason = %q, want %q", out.StopReason, stopEndTurn)
	}
	if gotPrompt != "what is in b.go?" {
		t.Errorf("prompt = %q", gotPrompt)
	}

	var kinds []string
	for _, u := range updates {
		if u.SessionID != sid {
			t.Errorf("update went to session %q, want %q", u.SessionID, sid)
		}
		m, _ := u.Update.(map[string]any)
		kinds = append(kinds, m["sessionUpdate"].(string))
	}
	want := []string{"agent_message_chunk", "tool_call", "tool_call_update", "agent_message_chunk"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("updates = %v, want %v", kinds, want)
	}
}

func TestPromptFlattensTheContentBlocks(t *testing.T) {
	// An editor sends more than text. A resource link becomes its path so the
	// model can Read it; an embedded resource's text is inlined, which is the
	// whole point of attaching the open buffer.
	cwd := t.TempDir()
	var got string
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			got = turn.Prompt
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	res := p.call("session/prompt", promptParams{
		SessionID: sid,
		Prompt: []contentBlock{
			textBlock("fix this"),
			{Type: "resource_link", URI: "file:///project/main.go", Name: "main.go"},
			{Type: "resource", Resource: &embeddedResource{
				URI: "file:///project/util.go", Text: "package util",
			}},
			{Type: "image", Data: "iVBORw0KGgo=", MimeType: "image/png"},
		},
	})
	if res.Error != nil {
		t.Fatalf("session/prompt: %+v", res.Error)
	}
	for _, want := range []string{"fix this", "file:///project/main.go", "package util"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "iVBORw0KGgo=") {
		t.Errorf("an image block Klaudia did not advertise reached the prompt:\n%s", got)
	}
}

func TestPromptErrors(t *testing.T) {
	cwd := t.TempDir()
	tests := []struct {
		name   string
		params func(sid string) promptParams
		want   int
	}{
		{
			name: "unknown session",
			params: func(string) promptParams {
				return promptParams{SessionID: "nope", Prompt: []contentBlock{textBlock("hi")}}
			},
			want: codeInvalidParams,
		},
		{
			name:   "nothing to say",
			params: func(sid string) promptParams { return promptParams{SessionID: sid} },
			want:   codeInvalidParams,
		},
		{
			name: "only content Klaudia cannot read",
			params: func(sid string) promptParams {
				return promptParams{SessionID: sid, Prompt: []contentBlock{
					{Type: "image", Data: "x", MimeType: "image/png"},
				}}
			},
			want: codeInvalidParams,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := serveAgent(t, Options{CWD: cwd})
			sid := p.newSession(cwd)
			res := p.call("session/prompt", tc.params(sid))
			if res.Error == nil || res.Error.Code != tc.want {
				t.Errorf("error = %+v, want code %d", res.Error, tc.want)
			}
		})
	}
}

func TestPromptCarriesHistoryForward(t *testing.T) {
	cwd := t.TempDir()
	var seen [][]anthropic.BetaMessageParam
	var mu sync.Mutex
	p := serveAgent(t, Options{
		CWD:     cwd,
		History: []anthropic.BetaMessageParam{{Role: anthropic.BetaMessageParamRoleUser}},
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			mu.Lock()
			seen = append(seen, turn.History)
			n := len(seen)
			mu.Unlock()
			out := make([]anthropic.BetaMessageParam, n+1)
			for i := range out {
				out[i] = anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleUser}
			}
			return agent.Result{Text: "ok", StopReason: "end_turn", Messages: out}, nil
		},
	})
	sid := p.newSession(cwd)
	for i := 0; i < 2; i++ {
		res := p.call("session/prompt", promptParams{
			SessionID: sid, Prompt: []contentBlock{textBlock("turn")},
		})
		if res.Error != nil {
			t.Fatalf("session/prompt: %+v", res.Error)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("ran %d turns, want 2", len(seen))
	}
	// A resumed transcript seeds the first session, and each turn continues
	// from the last one's messages rather than starting over.
	if len(seen[0]) != 1 {
		t.Errorf("first turn saw %d messages, want the resumed transcript's 1", len(seen[0]))
	}
	if len(seen[1]) != 2 {
		t.Errorf("second turn saw %d messages, want the first turn's 2", len(seen[1]))
	}
}

func TestResumedHistoryGoesToOneSessionOnly(t *testing.T) {
	// Resuming is about the process. A second thread opened in the same editor
	// is a new conversation, not a second copy of the resumed one.
	cwd := t.TempDir()
	var lens []int
	var mu sync.Mutex
	p := serveAgent(t, Options{
		CWD:     cwd,
		History: []anthropic.BetaMessageParam{{Role: anthropic.BetaMessageParamRoleUser}},
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			mu.Lock()
			lens = append(lens, len(turn.History))
			mu.Unlock()
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	first := p.newSession(cwd)
	res := p.call("session/new", newSessionParams{CWD: cwd})
	var out newSessionResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	for _, sid := range []string{first, out.SessionID} {
		if r := p.call("session/prompt", promptParams{
			SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
		}); r.Error != nil {
			t.Fatalf("session/prompt: %+v", r.Error)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(lens) != 2 || lens[0] != 1 || lens[1] != 0 {
		t.Errorf("history lengths = %v, want [1 0]", lens)
	}
}

func TestCancelAnsweringThePendingPrompt(t *testing.T) {
	// The spec is explicit: a cancelled turn answers the pending
	// session/prompt with stopReason "cancelled", not with an error. The
	// client asked for it, so it is not a failure.
	//
	// Both cases are real. The loop returns ctx.Err() when a cancel lands
	// mid-call, but when it lands at a turn boundary the loop finishes tidily
	// and returns a perfectly good Result with no error and whatever stop
	// reason it had — which is why the session records that a cancel happened
	// rather than inferring it from what came back.
	tests := []struct {
		name string
		ret  func(ctx context.Context) (agent.Result, error)
	}{
		{
			name: "the loop reports the cancellation",
			ret: func(ctx context.Context) (agent.Result, error) {
				return agent.Result{StopReason: "end_turn"}, ctx.Err()
			},
		},
		{
			name: "the loop finishes tidily and reports success",
			ret: func(context.Context) (agent.Result, error) {
				return agent.Result{Text: "stopped there", StopReason: "end_turn"}, nil
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			running := make(chan struct{})
			ret := tc.ret
			p := serveAgent(t, Options{
				CWD: cwd,
				Run: func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
					close(running)
					<-ctx.Done()
					return ret(ctx)
				},
			})
			sid := p.newSession(cwd)
			id := p.send("session/prompt", promptParams{
				SessionID: sid, Prompt: []contentBlock{textBlock("long job")},
			})
			<-running
			p.notify("session/cancel", cancelParams{SessionID: sid})

			res, _, _ := p.await(id)
			if res.Error != nil {
				t.Fatalf("a cancel must not produce an error response: %+v", res.Error)
			}
			var out promptResult
			if err := json.Unmarshal(res.Result, &out); err != nil {
				t.Fatal(err)
			}
			if out.StopReason != stopCancelled {
				t.Errorf("stopReason = %q, want %q", out.StopReason, stopCancelled)
			}
		})
	}
}

func TestANewPromptClearsTheCancelledFlag(t *testing.T) {
	// A cancelled turn must not make the next one report cancelled too: the
	// flag is per prompt, and a session the user stopped once is still usable.
	cwd := t.TempDir()
	running := make(chan struct{}, 1)
	var turns int
	var mu sync.Mutex
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
			mu.Lock()
			turns++
			first := turns == 1
			mu.Unlock()
			if first {
				running <- struct{}{}
				<-ctx.Done()
			}
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("one")},
	})
	<-running
	p.notify("session/cancel", cancelParams{SessionID: sid})
	p.await(id)

	res := p.call("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("two")},
	})
	if res.Error != nil {
		t.Fatalf("second prompt: %+v", res.Error)
	}
	var out promptResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.StopReason != stopEndTurn {
		t.Errorf("second stopReason = %q, want %q", out.StopReason, stopEndTurn)
	}
}

func TestCancelIsHonouredWhileQueued(t *testing.T) {
	// One prompt runs at a time across the process, so a second can be waiting.
	// A cancel while it waits must end it there rather than running a turn the
	// user has already abandoned.
	cwd := t.TempDir()
	release := make(chan struct{})
	started := make(chan string, 4)
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			started <- turn.Prompt
			<-release
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	first := p.newSession(cwd)
	res := p.call("session/new", newSessionParams{CWD: cwd})
	var out newSessionResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	second := out.SessionID

	firstID := p.send("session/prompt", promptParams{
		SessionID: first, Prompt: []contentBlock{textBlock("first")},
	})
	if got := <-started; got != "first" {
		t.Fatalf("first turn prompt = %q", got)
	}
	queuedID := p.send("session/prompt", promptParams{
		SessionID: second, Prompt: []contentBlock{textBlock("queued")},
	})
	// Let the queued prompt reach the turn gate before cancelling it. There is
	// no event for "is waiting", so this is a short sleep rather than a race
	// against a channel that does not exist.
	time.Sleep(50 * time.Millisecond)
	p.notify("session/cancel", cancelParams{SessionID: second})

	qres, _, _ := p.await(queuedID)
	var qout promptResult
	if err := json.Unmarshal(qres.Result, &qout); err != nil {
		t.Fatalf("queued prompt: %+v / %v", qres.Error, err)
	}
	if qout.StopReason != stopCancelled {
		t.Errorf("queued stopReason = %q, want %q", qout.StopReason, stopCancelled)
	}

	close(release)
	fres, _, _ := p.await(firstID)
	if fres.Error != nil {
		t.Fatalf("first prompt: %+v", fres.Error)
	}
	select {
	case got := <-started:
		t.Errorf("the cancelled prompt ran anyway: %q", got)
	default:
	}
}

func TestASecondPromptInOneSessionIsRefused(t *testing.T) {
	// The spec requires the agent to enforce one prompt per session. Queuing it
	// instead would have the editor's second message answered against a
	// conversation that had moved on.
	cwd := t.TempDir()
	running := make(chan struct{})
	release := make(chan struct{})
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(context.Context, agent.Turn) (agent.Result, error) {
			close(running)
			<-release
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	first := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("one")},
	})
	<-running
	res := p.call("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("two")},
	})
	if res.Error == nil || res.Error.Code != codeInvalidRequest {
		t.Errorf("error = %+v, want invalid request", res.Error)
	}
	close(release)
	p.await(first)
}

func TestSetMode(t *testing.T) {
	cwd := t.TempDir()
	var modes []permission.Mode
	var mu sync.Mutex
	release := make(chan struct{})
	p := serveAgent(t, Options{
		CWD:  cwd,
		Mode: permission.ModeAutonomous,
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			// Read the mode through the Turn, as the CLI's permission context
			// does, and read it twice around a change to prove it is live.
			mu.Lock()
			modes = append(modes, turn.Mode())
			mu.Unlock()
			<-release
			mu.Lock()
			modes = append(modes, turn.Mode())
			mu.Unlock()
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("go")},
	})
	// Wait for the turn to have read the mode once.
	for {
		mu.Lock()
		n := len(modes)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if res := p.call("session/set_mode", setModeParams{SessionID: sid, ModeID: "plan"}); res.Error != nil {
		t.Fatalf("session/set_mode: %+v", res.Error)
	}
	close(release)
	p.await(id)

	mu.Lock()
	defer mu.Unlock()
	if len(modes) != 2 || modes[0] != permission.ModeAutonomous || modes[1] != permission.ModePlan {
		t.Errorf("modes = %v, want [autonomous plan] — the mode must be read at check time", modes)
	}
}

func TestSetModeErrors(t *testing.T) {
	cwd := t.TempDir()
	p := serveAgent(t, Options{CWD: cwd})
	sid := p.newSession(cwd)
	if res := p.call("session/set_mode", setModeParams{SessionID: sid, ModeID: "acceptEdits"}); res.Error == nil {
		t.Error("a mode Klaudia retired should be refused, not accepted and ignored")
	}
	if res := p.call("session/set_mode", setModeParams{SessionID: "nope", ModeID: "plan"}); res.Error == nil {
		t.Error("an unknown session should be refused")
	}
}

func TestApprovedPlanLeavesPlanMode(t *testing.T) {
	// ExitPlanMode only asks; leaving plan mode is the frontend's job. Getting
	// this wrong is not cosmetic — the model approves its plan and then finds
	// every mutation still blocked, and loops.
	cwd := t.TempDir()
	modeAfter := make(chan permission.Mode, 1)
	p := serveAgent(t, Options{
		CWD:  cwd,
		Mode: permission.ModePlan,
		Run: func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
			ok, err := turn.Planner.ExitPlan(ctx, "1. do the thing")
			if err != nil || !ok {
				return agent.Result{}, err
			}
			modeAfter <- turn.Mode()
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("plan it")},
	})

	req := p.nextCall()
	if req.Method != "session/request_permission" {
		t.Fatalf("agent called %q, want session/request_permission", req.Method)
	}
	var ask requestPermissionParams
	if err := json.Unmarshal(req.Params, &ask); err != nil {
		t.Fatal(err)
	}
	if ask.ToolCall.ToolKind != kindSwitchMode {
		t.Errorf("kind = %q, want %q", ask.ToolCall.ToolKind, kindSwitchMode)
	}
	p.reply(req.ID, requestPermissionResult{Outcome: selected("approve")})

	select {
	case m := <-modeAfter:
		if m != permission.ModeAutonomous {
			t.Errorf("mode after approval = %q, want autonomous", m)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the turn never saw the approval")
	}

	_, updates, _ := p.await(id)
	var announced string
	for _, u := range updates {
		m, _ := u.Update.(map[string]any)
		if m["sessionUpdate"] == "current_mode_update" {
			announced, _ = m["currentModeId"].(string)
		}
	}
	if announced != string(permission.ModeAutonomous) {
		t.Errorf("the client was told the mode is %q, want autonomous", announced)
	}
}

func TestAskUserQuestionReachesTheEditor(t *testing.T) {
	// The whole point of an attended transport: the model's own questions get
	// to a human. Over stream-json this was dead for months because the CLI had
	// no wire form for it.
	cwd := t.TempDir()
	answer := make(chan string, 1)
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
			got, err := turn.Asker.Ask(ctx, "Which database?", []tools.AskOption{
				{Label: "Postgres"}, {Label: "SQLite"},
			})
			if err != nil {
				return agent.Result{}, err
			}
			answer <- got
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("choose")},
	})
	req := p.nextCall()
	var ask requestPermissionParams
	if err := json.Unmarshal(req.Params, &ask); err != nil {
		t.Fatal(err)
	}
	if ask.SessionID != sid {
		t.Errorf("the ask names session %q, want %q", ask.SessionID, sid)
	}
	p.reply(req.ID, requestPermissionResult{Outcome: selected("1")})
	select {
	case got := <-answer:
		if got != "SQLite" {
			t.Errorf("answer = %q, want SQLite", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the turn never got its answer")
	}
	p.await(id)
}

func TestSilentTurnExplainsItself(t *testing.T) {
	// A refusal or an output limit completes cleanly and says nothing. ACP has
	// no status for either, so the explanation has to go out as text —
	// otherwise the editor shows an empty reply, which is indistinguishable
	// from a bug and was reported as one.
	cwd := t.TempDir()
	tests := []struct {
		name string
		// streamed is the text the loop emitted as it went, as a real one does.
		streamed   string
		result     agent.Result
		wantReason string
		wantText   string
	}{
		{
			name:       "a refusal",
			result:     agent.Result{StopReason: "refusal"},
			wantReason: stopRefusal,
			wantText:   "declined to respond",
		},
		{
			name:       "the output limit with nothing said",
			result:     agent.Result{StopReason: "max_tokens"},
			wantReason: stopMaxTokens,
			wantText:   "before saying anything",
		},
		{
			name:       "the output limit mid-answer",
			streamed:   "here is the first ha",
			result:     agent.Result{Text: "here is the first ha", StopReason: "max_tokens"},
			wantReason: stopMaxTokens,
			wantText:   "cut off at the output limit",
		},
		{
			name: "a hook that stopped the prompt",
			// The loop returns early with its reason in Text and never emits
			// it, so the agent has to.
			result:     agent.Result{StopReason: "blocked_by_hook", Text: "prompt blocked by hook"},
			wantReason: stopEndTurn,
			wantText:   "blocked by hook",
		},
		{
			name:       "an ordinary answer needs no note",
			streamed:   "here you go",
			result:     agent.Result{Text: "here you go", StopReason: "end_turn"},
			wantReason: stopEndTurn,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, streamed := tc.result, tc.streamed
			p := serveAgent(t, Options{
				CWD: cwd,
				Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
					if streamed != "" {
						turn.Emit(agent.Event{Type: "assistant", Text: streamed})
					}
					return res, nil
				},
			})
			sid := p.newSession(cwd)
			id := p.send("session/prompt", promptParams{
				SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
			})
			reply, updates, _ := p.await(id)
			var out promptResult
			if err := json.Unmarshal(reply.Result, &out); err != nil {
				t.Fatalf("%+v / %v", reply.Error, err)
			}
			if out.StopReason != tc.wantReason {
				t.Errorf("stopReason = %q, want %q", out.StopReason, tc.wantReason)
			}
			var text string
			for _, u := range updates {
				m, _ := u.Update.(map[string]any)
				if m["sessionUpdate"] == "agent_message_chunk" {
					c, _ := m["content"].(map[string]any)
					text += c["text"].(string)
				}
			}
			extra := strings.TrimPrefix(text, tc.streamed)
			if tc.wantText == "" {
				if extra != "" {
					t.Errorf("got an unsolicited message %q", extra)
				}
				return
			}
			if !strings.Contains(extra, tc.wantText) {
				t.Errorf("message = %q, want it to contain %q", extra, tc.wantText)
			}
		})
	}
}

func TestResultTextIsNotSentTwice(t *testing.T) {
	// The streamed deltas and Result.Text are the same words. Emitting both
	// would show the whole answer twice, which is what a naive "always send
	// Result.Text at the end" does.
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			turn.Emit(agent.Event{Type: "assistant", Text: "hello "})
			turn.Emit(agent.Event{Type: "assistant", Text: "world"})
			return agent.Result{Text: "hello world", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
	})
	_, updates, _ := p.await(id)
	var text string
	for _, u := range updates {
		m, _ := u.Update.(map[string]any)
		if m["sessionUpdate"] == "agent_message_chunk" {
			c, _ := m["content"].(map[string]any)
			text += c["text"].(string)
		}
	}
	if text != "hello world" {
		t.Errorf("text = %q, want %q", text, "hello world")
	}
}

func TestRunFailureIsAnErrorResponse(t *testing.T) {
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(context.Context, agent.Turn) (agent.Result, error) {
			return agent.Result{}, io.ErrUnexpectedEOF
		},
	})
	sid := p.newSession(cwd)
	res := p.call("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
	})
	if res.Error == nil || res.Error.Code != codeInternalError {
		t.Errorf("error = %+v, want an internal error", res.Error)
	}
}

func TestEditorMCPServersAreReportedNotIgnored(t *testing.T) {
	// Klaudia connects the servers in .mcp.json, so the editor's list cannot be
	// honoured. Saying so is the difference between a missing tool the user can
	// explain and one they cannot.
	cwd := t.TempDir()
	logged := make(chan string, 4)
	p := serveAgent(t, Options{CWD: cwd, Log: func(m string) { logged <- m }})
	p.call("initialize", initializeParams{ProtocolVersion: 1})
	res := p.call("session/new", newSessionParams{
		CWD:        cwd,
		MCPServers: []json.RawMessage{json.RawMessage(`{"name":"gsx"}`)},
	})
	if res.Error != nil {
		t.Fatalf("session/new: %+v", res.Error)
	}
	select {
	case m := <-logged:
		if !strings.Contains(m, ".mcp.json") {
			t.Errorf("log = %q, want it to name .mcp.json", m)
		}
	case <-time.After(time.Second):
		t.Error("the editor's MCP servers were dropped in silence")
	}
}

func TestAuthenticateIsAnswered(t *testing.T) {
	// No auth methods are advertised, so a well-behaved client never calls
	// this. Answering null keeps one that calls it unconditionally working.
	p := serveAgent(t, Options{})
	res := p.call("authenticate", map[string]any{"methodId": "none"})
	if res.Error != nil {
		t.Errorf("authenticate: %+v", res.Error)
	}
}

func TestSamePath(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "identical", a: "/a/b", b: "/a/b", want: true},
		{name: "a trailing slash is the same directory", a: "/a/b/", b: "/a/b", want: true},
		{name: "a dot segment is the same directory", a: "/a/./b", b: "/a/b", want: true},
		{name: "different", a: "/a/b", b: "/a/c", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := samePath(tc.a, tc.b); got != tc.want {
				t.Errorf("samePath(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestSamePathSeesThroughASymlink(t *testing.T) {
	// macOS hands an editor /var and Klaudia /private/var for the same
	// directory. Comparing the strings would refuse every session on a temp
	// directory.
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := symlink(dir, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	if !samePath(link, dir) {
		t.Errorf("samePath(%q, %q) = false, want true", link, dir)
	}
}

func symlink(target, link string) error { return os.Symlink(target, link) }
