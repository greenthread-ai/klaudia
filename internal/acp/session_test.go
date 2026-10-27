package acp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// fakeTranscript records what a session persisted, and whether it was closed.
type fakeTranscript struct {
	id string

	mu       sync.Mutex
	recorded []string
	closed   bool
}

func (f *fakeTranscript) Record(role string, message json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recorded = append(f.recorded, role+":"+string(message))
	return nil
}

func (f *fakeTranscript) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeTranscript) snapshot() ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.recorded...), f.closed
}

// transcriptStore hands out one fakeTranscript per session id.
type transcriptStore struct {
	mu   sync.Mutex
	open map[string]*fakeTranscript
}

func newTranscriptStore() *transcriptStore {
	return &transcriptStore{open: map[string]*fakeTranscript{}}
}

func (s *transcriptStore) forSession(id string) Transcript {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := &fakeTranscript{id: id}
	s.open[id] = f
	return f
}

func (s *transcriptStore) get(t *testing.T, id string) *fakeTranscript {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.open[id]
	if !ok {
		t.Fatalf("no transcript was opened for session %s", id)
	}
	return f
}

func TestSessionCapabilitiesAreOnlyAdvertisedWhenBacked(t *testing.T) {
	// A client told loadSession or session/list is supported, and then refused
	// every call, cannot tell that apart from a broken agent.
	tests := []struct {
		name      string
		opts      Options
		wantLoad  bool
		wantList  bool
		wantClose bool
	}{
		{
			name:      "no store behind either",
			opts:      Options{},
			wantClose: true,
		},
		{
			name: "both backed",
			opts: Options{
				LoadHistory:  func(string) ([]anthropic.BetaMessageParam, error) { return nil, nil },
				ListSessions: func() []SessionSummary { return nil },
			},
			wantLoad:  true,
			wantList:  true,
			wantClose: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := serveAgent(t, tc.opts)
			res := p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
			var out initializeResult
			if err := json.Unmarshal(res.Result, &out); err != nil {
				t.Fatal(err)
			}
			caps := out.AgentCapabilities
			if caps.LoadSession != tc.wantLoad {
				t.Errorf("loadSession = %v, want %v", caps.LoadSession, tc.wantLoad)
			}
			if caps.SessionCapabilities == nil {
				t.Fatal("no sessionCapabilities")
			}
			if (caps.SessionCapabilities.List != nil) != tc.wantList {
				t.Errorf("sessionCapabilities.list = %v, want %v",
					caps.SessionCapabilities.List, tc.wantList)
			}
			if (caps.SessionCapabilities.Close != nil) != tc.wantClose {
				t.Errorf("sessionCapabilities.close = %v, want %v",
					caps.SessionCapabilities.Close, tc.wantClose)
			}
		})
	}
}

func TestSessionCapabilitiesAreObjectsNotBooleans(t *testing.T) {
	// SessionCapabilities' members are present-or-absent objects: {} means
	// supported. A boolean there is a decode error at the client, and it is the
	// kind of mistake a Go struct hides until something else parses the JSON.
	b, err := json.Marshal(sessionCapabilities{Close: &sessionMethodCapability{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"close":{}}` {
		t.Errorf("got %s, want %s", got, `{"close":{}}`)
	}
}

func TestLoadSessionReplaysFromTheStore(t *testing.T) {
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD: cwd,
		LoadHistory: func(id string) ([]anthropic.BetaMessageParam, error) {
			if id != "sess_old" {
				return nil, errors.New("no such session " + id)
			}
			return []anthropic.BetaMessageParam{
				userText("hello"),
				assistantText("hi"),
			}, nil
		},
	})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})

	id := p.send("session/load", loadSessionParams{
		SessionID: "sess_old", CWD: cwd, MCPServers: []json.RawMessage{},
	})
	res, updates, _ := p.await(id)
	if res.Error != nil {
		t.Fatalf("session/load: %+v", res.Error)
	}
	// The spec requires the conversation to have been sent by the time the
	// request returns, which is why these are collected *before* the response.
	var kinds []string
	for _, u := range updates {
		m, _ := u.Update.(map[string]any)
		kinds = append(kinds, m["sessionUpdate"].(string))
		if u.SessionID != "sess_old" {
			t.Errorf("update went to session %q", u.SessionID)
		}
	}
	want := []string{"user_message_chunk", "agent_message_chunk", "available_commands_update"}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("updates = %v, want %v", kinds, want)
	}

	var out loadSessionResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if out.Modes == nil || out.Modes.CurrentModeID == "" {
		t.Errorf("modes = %+v, want the loaded session's mode", out.Modes)
	}
}

func TestLoadSessionContinuesTheLoadedConversation(t *testing.T) {
	// Replaying it to the client is half the job; the next prompt has to
	// actually continue it, or the user sees their history and the model does
	// not.
	cwd := t.TempDir()
	var got []anthropic.BetaMessageParam
	p := serveAgent(t, Options{
		CWD: cwd,
		LoadHistory: func(string) ([]anthropic.BetaMessageParam, error) {
			return []anthropic.BetaMessageParam{userText("hello"), assistantText("hi")}, nil
		},
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			got = turn.History
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
	if res := p.call("session/load", loadSessionParams{SessionID: "sess_old", CWD: cwd}); res.Error != nil {
		t.Fatalf("session/load: %+v", res.Error)
	}
	if res := p.call("session/prompt", promptParams{
		SessionID: "sess_old", Prompt: []contentBlock{textBlock("and now?")},
	}); res.Error != nil {
		t.Fatalf("session/prompt: %+v", res.Error)
	}
	if len(got) != 2 {
		t.Errorf("the turn saw %d messages, want the loaded 2", len(got))
	}
}

func TestLoadSessionOfALiveThreadReplaysMemory(t *testing.T) {
	// A client reconnecting to a thread this process still has open must get
	// what is in memory, not what is on disk: the last turn may not be flushed,
	// so the store is strictly older.
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD: cwd,
		LoadHistory: func(string) ([]anthropic.BetaMessageParam, error) {
			return []anthropic.BetaMessageParam{userText("stale, from disk")}, nil
		},
		Run: func(context.Context, agent.Turn) (agent.Result, error) {
			return agent.Result{
				Text: "ok", StopReason: "end_turn",
				Messages: []anthropic.BetaMessageParam{userText("live, in memory")},
			}, nil
		},
	})
	sid := p.newSession(cwd)
	if res := p.call("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
	}); res.Error != nil {
		t.Fatalf("session/prompt: %+v", res.Error)
	}

	id := p.send("session/load", loadSessionParams{SessionID: sid, CWD: cwd})
	res, updates, _ := p.await(id)
	if res.Error != nil {
		t.Fatalf("session/load: %+v", res.Error)
	}
	var texts []string
	for _, u := range updates {
		m, _ := u.Update.(map[string]any)
		c, _ := m["content"].(map[string]any)
		if s, ok := c["text"].(string); ok {
			texts = append(texts, s)
		}
	}
	found := false
	for _, s := range texts {
		if s == "live, in memory" {
			found = true
		}
		if s == "stale, from disk" {
			t.Error("session/load read the store for a session that is still open")
		}
	}
	if !found {
		t.Errorf("replayed %v, want the in-memory history", texts)
	}
}

func TestLoadSessionErrors(t *testing.T) {
	cwd := t.TempDir()
	tests := []struct {
		name     string
		opts     Options
		params   any
		wantCode int
	}{
		{
			name:     "no store behind it",
			opts:     Options{CWD: cwd},
			params:   loadSessionParams{SessionID: "x", CWD: cwd},
			wantCode: codeMethodNotFound,
		},
		{
			name: "no session id",
			opts: Options{CWD: cwd, LoadHistory: func(string) ([]anthropic.BetaMessageParam, error) {
				return nil, nil
			}},
			params:   loadSessionParams{CWD: cwd},
			wantCode: codeInvalidParams,
		},
		{
			name: "a different project root",
			opts: Options{CWD: cwd, LoadHistory: func(string) ([]anthropic.BetaMessageParam, error) {
				return nil, nil
			}},
			params:   loadSessionParams{SessionID: "x", CWD: cwd + "/elsewhere"},
			wantCode: codeInvalidParams,
		},
		{
			name: "an id the store does not have",
			// invalid_params, not internal: the id came from the client, so
			// this is the client's mistake to correct.
			opts: Options{CWD: cwd, LoadHistory: func(string) ([]anthropic.BetaMessageParam, error) {
				return nil, errors.New("no such session")
			}},
			params:   loadSessionParams{SessionID: "gone", CWD: cwd},
			wantCode: codeInvalidParams,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := serveAgent(t, tc.opts)
			p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
			res := p.call("session/load", tc.params)
			if res.Error == nil {
				t.Fatalf("got result %s, want an error", res.Result)
			}
			if res.Error.Code != tc.wantCode {
				t.Errorf("code = %d, want %d (%s)", res.Error.Code, tc.wantCode, res.Error.Message)
			}
		})
	}
}

func TestLoadSessionLogsATranscriptThatReplaysToNothing(t *testing.T) {
	// Otherwise a format change is indistinguishable from an empty session:
	// the client gets a successful load and no content, and nobody finds out.
	cwd := t.TempDir()
	var logged []string
	var mu sync.Mutex
	p := serveAgent(t, Options{
		CWD: cwd,
		LoadHistory: func(string) ([]anthropic.BetaMessageParam, error) {
			// One message, no displayable blocks.
			return []anthropic.BetaMessageParam{{Role: anthropic.BetaMessageParamRoleUser}}, nil
		},
		Log: func(m string) {
			mu.Lock()
			defer mu.Unlock()
			logged = append(logged, m)
		},
	})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
	if res := p.call("session/load", loadSessionParams{SessionID: "sess_x", CWD: cwd}); res.Error != nil {
		t.Fatalf("session/load: %+v", res.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, m := range logged {
		if strings.Contains(m, "replayed to nothing") {
			return
		}
	}
	t.Errorf("nothing was logged about an undisplayable transcript: %v", logged)
}

func TestListSessions(t *testing.T) {
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD: cwd,
		ListSessions: func() []SessionSummary {
			return []SessionSummary{
				{ID: "sess_2", Title: "fix the parser", UpdatedAt: "2026-10-06T12:00:00Z"},
				{ID: "sess_1"},
			}
		},
	})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
	res := p.call("session/list", listSessionsParams{})
	if res.Error != nil {
		t.Fatalf("session/list: %+v", res.Error)
	}
	want := `{"sessions":[
		{"sessionId":"sess_2","cwd":"` + cwd + `","title":"fix the parser",
		 "updatedAt":"2026-10-06T12:00:00Z"},
		{"sessionId":"sess_1","cwd":"` + cwd + `"}
	],"nextCursor":null}`
	var got any
	if err := json.Unmarshal(res.Result, &got); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(t, got, want) {
		t.Errorf("got  %s\nwant %s", res.Result, want)
	}
}

func TestListSessionsCWDFilter(t *testing.T) {
	cwd := t.TempDir()
	other := t.TempDir()
	p := serveAgent(t, Options{
		CWD:          cwd,
		ListSessions: func() []SessionSummary { return []SessionSummary{{ID: "sess_1"}} },
	})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})

	// Somewhere else is answered with an empty list rather than an error: "no
	// sessions there" is true, and this process genuinely has none.
	res := p.call("session/list", listSessionsParams{CWD: &other})
	if res.Error != nil {
		t.Fatalf("session/list: %+v", res.Error)
	}
	var out listSessionsResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Sessions) != 0 {
		t.Errorf("sessions = %+v, want none for another root", out.Sessions)
	}
	// An empty array, not null: a client distinguishes "none" from "no answer".
	if !strings.Contains(string(res.Result), `"sessions":[]`) {
		t.Errorf("result = %s, want an empty sessions array", res.Result)
	}
}

func TestListSessionsWithNoStoreIsMethodNotFound(t *testing.T) {
	p := serveAgent(t, Options{})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
	res := p.call("session/list", listSessionsParams{})
	if res.Error == nil || res.Error.Code != codeMethodNotFound {
		t.Errorf("error = %+v, want method not found", res.Error)
	}
}

func TestCloseSession(t *testing.T) {
	cwd := t.TempDir()
	store := newTranscriptStore()
	p := serveAgent(t, Options{CWD: cwd, Transcript: store.forSession})
	sid := p.newSession(cwd)

	res := p.call("session/close", closeSessionParams{SessionID: sid})
	if res.Error != nil {
		t.Fatalf("session/close: %+v", res.Error)
	}
	if _, closed := store.get(t, sid).snapshot(); !closed {
		t.Error("the transcript was left open, so its buffered writes are lost")
	}
	// Closing is closing: the session is gone, so a prompt for it is an unknown
	// session rather than a silently resurrected thread.
	if r := p.call("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
	}); r.Error == nil {
		t.Error("prompting a closed session succeeded")
	}
}

func TestCloseSessionOfAnUnknownIDIsNotAnError(t *testing.T) {
	// A client closing a thread twice, or one this process never had, has got
	// what it asked for.
	p := serveAgent(t, Options{})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
	if res := p.call("session/close", closeSessionParams{SessionID: "nope"}); res.Error != nil {
		t.Errorf("session/close of an unknown id: %+v", res.Error)
	}
}

func TestServeClosesTranscriptsTheClientLeftOpen(t *testing.T) {
	// A client exiting is the normal way an ACP connection ends. Leaving the
	// files open loses whatever was buffered.
	//
	// Built by hand rather than with serveAgent, because this test has to end
	// the connection itself and wait for Serve to return; serveAgent's cleanup
	// owns both of those and would find the result already consumed.
	cwd := t.TempDir()
	store := newTranscriptStore()
	sink := newFrameSink()
	inR, inW := io.Pipe()
	a := New(Options{
		CWD:        cwd,
		Transcript: store.forSession,
		Run: func(context.Context, agent.Turn) (agent.Result, error) {
			return agent.Result{StopReason: "end_turn"}, nil
		},
		NewSessionID: func() string { return "sess_1" },
	}, sink)
	done := make(chan error, 1)
	go func() { done <- a.Serve(context.Background(), inR) }()

	p := &peer{t: t, in: inW, sink: sink, done: done}
	sid := p.newSession(cwd)
	if _, closed := store.get(t, sid).snapshot(); closed {
		t.Fatal("the transcript was closed before the connection ended")
	}

	if err := inW.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after stdin closed")
	}
	if _, closed := store.get(t, sid).snapshot(); !closed {
		t.Error("the transcript was not closed when the connection ended")
	}
}

func TestEachSessionRecordsToItsOwnTranscript(t *testing.T) {
	// One process-wide recorder had two editor threads appending to the same
	// file, interleaving two conversations into one — so session/load replayed
	// something that never happened.
	cwd := t.TempDir()
	store := newTranscriptStore()
	p := serveAgent(t, Options{
		CWD:        cwd,
		Transcript: store.forSession,
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			if turn.Recorder == nil {
				t.Error("the turn got no recorder, so nothing is persisted")
				return agent.Result{StopReason: "end_turn"}, nil
			}
			if err := turn.Recorder.Record("user", json.RawMessage(`"`+turn.Prompt+`"`)); err != nil {
				t.Error(err)
			}
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

	for sid, text := range map[string]string{first: "one", second: "two"} {
		if r := p.call("session/prompt", promptParams{
			SessionID: sid, Prompt: []contentBlock{textBlock(text)},
		}); r.Error != nil {
			t.Fatalf("session/prompt: %+v", r.Error)
		}
	}
	for sid, want := range map[string]string{first: `user:"one"`, second: `user:"two"`} {
		got, _ := store.get(t, sid).snapshot()
		if len(got) != 1 || got[0] != want {
			t.Errorf("session %s recorded %v, want exactly [%s]", sid, got, want)
		}
	}
}

func TestEachSessionHasItsOwnPlan(t *testing.T) {
	// The plan store was built once per process and wired into the shared
	// registry, so two editor threads shared one step list — and, after
	// approval, one plan file keyed by the process's session id rather than
	// either thread's. Each turn must carry its own session's store.
	tests := []struct {
		name    string
		factory bool // whether the CLI supplied Options.Plan
	}{
		{name: "stores from the CLI's factory", factory: true},
		{name: "in-memory stores when no factory is given", factory: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			var mu sync.Mutex
			opened := map[string]*plan.Store{}
			byPrompt := map[string]*plan.Store{}
			opts := Options{
				CWD: cwd,
				Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
					mu.Lock()
					defer mu.Unlock()
					byPrompt[turn.Prompt] = turn.Plan
					if turn.Plan != nil {
						// What TodoWrite does through tools.Context.Plans.
						_ = turn.Plan.Replace([]plan.Item{{Content: turn.Prompt, Status: plan.StatusPending}})
					}
					return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
				},
			}
			if tc.factory {
				opts.Plan = func(id string) *plan.Store {
					mu.Lock()
					defer mu.Unlock()
					s := plan.New("")
					opened[id] = s
					return s
				}
			}
			p := serveAgent(t, opts)
			first := p.newSession(cwd)
			res := p.call("session/new", newSessionParams{CWD: cwd})
			var out newSessionResult
			if err := json.Unmarshal(res.Result, &out); err != nil {
				t.Fatal(err)
			}
			second := out.SessionID

			for _, step := range []struct{ sid, text string }{{first, "one"}, {second, "two"}} {
				if r := p.call("session/prompt", promptParams{
					SessionID: step.sid, Prompt: []contentBlock{textBlock(step.text)},
				}); r.Error != nil {
					t.Fatalf("session/prompt: %+v", r.Error)
				}
			}

			mu.Lock()
			defer mu.Unlock()
			one, two := byPrompt["one"], byPrompt["two"]
			if one == nil || two == nil {
				t.Fatalf("a turn carried no plan store (one=%v two=%v); the shared one would be used", one, two)
			}
			if one == two {
				t.Fatal("both sessions' turns carried the same plan store")
			}
			if got := one.Items(); len(got) != 1 || got[0].Content != "one" {
				t.Errorf("the first session's plan = %v, want only its own step", got)
			}
			if tc.factory && (opened[first] != one || opened[second] != two) {
				t.Error("a session's turns did not use the store opened for that session's id")
			}
		})
	}
}

func TestUsageUpdateReportsResidentTokens(t *testing.T) {
	// ACP's usage_update is "tokens resident in the context, out of this many",
	// not a delta. Forwarding the loop's per-call deltas would report a growing
	// total against a fixed window and show a session at 300% of its context.
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD:           cwd,
		ContextWindow: 200000,
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			turn.Emit(agent.Event{Type: "usage", InputDelta: 1200, OutputDelta: 300, TurnDelta: 1})
			return agent.Result{
				Text: "ok", StopReason: "end_turn",
				Messages: []anthropic.BetaMessageParam{userText(strings.Repeat("word ", 500))},
			}, nil
		},
	})
	sid := p.newSession(cwd)
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
	})
	res, updates, _ := p.await(id)
	if res.Error != nil {
		t.Fatalf("session/prompt: %+v", res.Error)
	}

	var usages []map[string]any
	for _, u := range updates {
		m, _ := u.Update.(map[string]any)
		if m["sessionUpdate"] == "usage_update" {
			usages = append(usages, m)
		}
	}
	if len(usages) != 1 {
		t.Fatalf("got %d usage updates, want exactly one per turn", len(usages))
	}
	if usages[0]["size"].(float64) != 200000 {
		t.Errorf("size = %v, want the context window", usages[0]["size"])
	}
	// The resident figure comes from the history, so it is nothing like the
	// 1500 the deltas carried.
	if used := usages[0]["used"].(float64); used <= 0 || used == 1500 {
		t.Errorf("used = %v, want an estimate of the resulting history", used)
	}
}

func TestNoUsageUpdateWithoutAContextWindow(t *testing.T) {
	// A percentage of an unknown total is worse than no gauge.
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD: cwd,
		Run: func(context.Context, agent.Turn) (agent.Result, error) {
			return agent.Result{
				Text: "ok", StopReason: "end_turn",
				Messages: []anthropic.BetaMessageParam{userText("hi")},
			}, nil
		},
	})
	sid := p.newSession(cwd)
	id := p.send("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("hi")},
	})
	res, updates, _ := p.await(id)
	if res.Error != nil {
		t.Fatalf("session/prompt: %+v", res.Error)
	}
	for _, u := range updates {
		m, _ := u.Update.(map[string]any)
		if m["sessionUpdate"] == "usage_update" {
			t.Errorf("a usage update was sent with no window to measure against: %v", m)
		}
	}
}

func TestAvailableCommandsNameTheirSession(t *testing.T) {
	// Sent after session/new's response, because the client learns the session
	// id from that response: a notification sent first names a session it
	// cannot route to.
	cwd := t.TempDir()
	p := serveAgent(t, Options{
		CWD: cwd,
		Commands: []Command{{
			Name: "review", Description: "review a file", Hint: "path",
			Render: func(a string) string { return "Review " + a },
		}},
	})
	p.call("initialize", initializeParams{ProtocolVersion: protocolVersion})
	id := p.send("session/new", newSessionParams{CWD: cwd})
	res, before, _ := p.await(id)
	if res.Error != nil {
		t.Fatalf("session/new: %+v", res.Error)
	}
	for _, u := range before {
		m, _ := u.Update.(map[string]any)
		if m["sessionUpdate"] == "available_commands_update" {
			t.Error("the commands went out before the response, naming a session the client cannot route")
		}
	}
	n := p.awaitUpdate("available_commands_update")
	var out newSessionResult
	if err := json.Unmarshal(res.Result, &out); err != nil {
		t.Fatal(err)
	}
	if n.SessionID != out.SessionID {
		t.Errorf("commands went to session %q, want %q", n.SessionID, out.SessionID)
	}
	m, _ := n.Update.(map[string]any)
	want := `{"sessionUpdate":"available_commands_update","availableCommands":[
		{"name":"review","description":"review a file","input":{"hint":"path"}}]}`
	if !jsonEqual(t, m, want) {
		t.Errorf("got  %v\nwant %s", m, want)
	}
}

func TestACommandFromThePickerIsExpandedBeforeTheTurn(t *testing.T) {
	// A client's command picker sends the command as ordinary prompt text, so
	// "/review foo.go" arrives as a prompt and nothing else would expand it.
	cwd := t.TempDir()
	var gotPrompt string
	p := serveAgent(t, Options{
		CWD: cwd,
		Commands: []Command{{
			Name:   "review",
			Render: func(a string) string { return "Review this file: " + a },
		}},
		Run: func(_ context.Context, turn agent.Turn) (agent.Result, error) {
			gotPrompt = turn.Prompt
			return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
		},
	})
	sid := p.newSession(cwd)
	if res := p.call("session/prompt", promptParams{
		SessionID: sid, Prompt: []contentBlock{textBlock("/review foo.go")},
	}); res.Error != nil {
		t.Fatalf("session/prompt: %+v", res.Error)
	}
	if gotPrompt != "Review this file: foo.go" {
		t.Errorf("prompt = %q, want the rendered command", gotPrompt)
	}
}
