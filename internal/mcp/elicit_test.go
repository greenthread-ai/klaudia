package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// scriptedAsker answers each question from a queue and records what it was
// asked, so a test can assert on the prompt text and the field order as well as
// on the result.
type scriptedAsker struct {
	answers   []string
	questions []string
	options   [][]tools.AskOption
	err       error
}

func (a *scriptedAsker) Ask(_ context.Context, question string, options []tools.AskOption) (string, error) {
	a.questions = append(a.questions, question)
	a.options = append(a.options, options)
	if a.err != nil {
		return "", a.err
	}
	if len(a.answers) == 0 {
		return "", errors.New("scriptedAsker: ran out of answers")
	}
	ans := a.answers[0]
	a.answers = a.answers[1:]
	return ans, nil
}

// schemaOf parses a JSON schema literal the way one arrives from the wire: as a
// decoded map, with key order already lost.
func schemaOf(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("schema literal: %v", err)
	}
	return v
}

func TestElicitForm(t *testing.T) {
	tests := []struct {
		name    string
		params  *mcpsdk.ElicitParams
		schema  string
		answers []string
		want    string         // expected action
		content map[string]any // expected content when accepted
	}{
		{
			name:    "no fields is a confirmation accepted by yes",
			params:  &mcpsdk.ElicitParams{Message: "Delete the stale branches?"},
			answers: []string{"Yes"},
			want:    actionAccept,
			content: map[string]any{},
		},
		{
			name:    "no fields declined by no",
			params:  &mcpsdk.ElicitParams{Message: "Delete the stale branches?"},
			answers: []string{"No"},
			want:    actionDecline,
		},
		{
			name:    "confirmation understands a typed answer",
			params:  &mcpsdk.ElicitParams{Message: "Proceed?"},
			answers: []string{"yep"},
			want:    actionDecline,
		},
		{
			name:    "required string is collected",
			schema:  `{"type":"object","properties":{"token":{"type":"string"}},"required":["token"]}`,
			answers: []string{"ghp_abc"},
			want:    actionAccept,
			content: map[string]any{"token": "ghp_abc"},
		},
		{
			name:    "empty answer to a required string cancels",
			schema:  `{"type":"object","properties":{"token":{"type":"string"}},"required":["token"]}`,
			answers: []string{"   "},
			want:    actionCancel,
		},
		{
			name:    "optional field may be skipped",
			schema:  `{"type":"object","properties":{"note":{"type":"string"}}}`,
			answers: []string{"Skip"},
			want:    actionAccept,
			content: map[string]any{},
		},
		{
			name:    "cancel abandons the whole form",
			schema:  `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["a","b"]}`,
			answers: []string{"first", "Cancel"},
			want:    actionCancel,
		},
		{
			name:    "boolean yes becomes true",
			schema:  `{"type":"object","properties":{"force":{"type":"boolean"}},"required":["force"]}`,
			answers: []string{"Yes"},
			want:    actionAccept,
			content: map[string]any{"force": true},
		},
		{
			name:    "boolean no becomes false",
			schema:  `{"type":"object","properties":{"force":{"type":"boolean"}},"required":["force"]}`,
			answers: []string{"No"},
			want:    actionAccept,
			content: map[string]any{"force": false},
		},
		{
			name:    "unreadable boolean cancels rather than guessing",
			schema:  `{"type":"object","properties":{"force":{"type":"boolean"}},"required":["force"]}`,
			answers: []string{"maybe later"},
			want:    actionCancel,
		},
		{
			name:    "integer is typed as an integer",
			schema:  `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`,
			answers: []string{"42"},
			want:    actionAccept,
			content: map[string]any{"n": int64(42)},
		},
		{
			name:    "whole float is accepted for an integer",
			schema:  `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`,
			answers: []string{"3.0"},
			want:    actionAccept,
			content: map[string]any{"n": int64(3)},
		},
		{
			name:    "fractional answer to an integer cancels",
			schema:  `{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`,
			answers: []string{"3.5"},
			want:    actionCancel,
		},
		{
			name:    "number keeps its fraction",
			schema:  `{"type":"object","properties":{"ratio":{"type":"number"}},"required":["ratio"]}`,
			answers: []string{"0.25"},
			want:    actionAccept,
			content: map[string]any{"ratio": 0.25},
		},
		{
			name:    "enum answered by value",
			schema:  `{"type":"object","properties":{"env":{"type":"string","enum":["dev","prod"]}},"required":["env"]}`,
			answers: []string{"prod"},
			want:    actionAccept,
			content: map[string]any{"env": "prod"},
		},
		{
			name:    "enum answered by display name sends the wire value",
			schema:  `{"type":"object","properties":{"env":{"type":"string","enum":["dev","prod"],"enumNames":["Development","Production"]}},"required":["env"]}`,
			answers: []string{"Production"},
			want:    actionAccept,
			content: map[string]any{"env": "prod"},
		},
		{
			name:    "answer outside the enum cancels",
			schema:  `{"type":"object","properties":{"env":{"type":"string","enum":["dev","prod"]}},"required":["env"]}`,
			answers: []string{"staging"},
			want:    actionCancel,
		},
		{
			name:    "integer enum goes back as a number",
			schema:  `{"type":"object","properties":{"n":{"type":"integer","enum":[1,2]}},"required":["n"]}`,
			answers: []string{"2"},
			want:    actionAccept,
			content: map[string]any{"n": int64(2)},
		},
		{
			name:   "url mode is declined, not half-supported",
			params: &mcpsdk.ElicitParams{Mode: "url", Message: "Authorise", URL: "https://example.test/auth"},
			want:   actionDecline,
		},
		{
			name:   "unreadable schema is declined without prompting",
			params: &mcpsdk.ElicitParams{Message: "hi", RequestedSchema: json.RawMessage(`{"properties":[]}`)},
			want:   actionDecline,
		},
		{
			name:    "titled oneOf sends the const for a picked title",
			schema:  `{"type":"object","properties":{"env":{"type":"string","oneOf":[{"const":"prd","title":"Production"},{"const":"stg","title":"Staging"}]}},"required":["env"]}`,
			answers: []string{"Staging"},
			want:    actionAccept,
			content: map[string]any{"env": "stg"},
		},
		{
			name:    "multi-select takes a comma list of titles or values",
			schema:  `{"type":"object","properties":{"tags":{"type":"array","items":{"anyOf":[{"const":"a","title":"Alpha"},{"const":"b","title":"Beta"}]}}},"required":["tags"]}`,
			answers: []string{"Alpha, b, alpha"},
			want:    actionAccept,
			content: map[string]any{"tags": []any{"a", "b"}},
		},
		{
			name:    "multi-select outside maxItems cancels",
			schema:  `{"type":"object","properties":{"tags":{"type":"array","maxItems":1,"items":{"type":"string","enum":["a","b"]}}},"required":["tags"]}`,
			answers: []string{"a,b"},
			want:    actionCancel,
		},
		{
			name:    "use default sends the declared default",
			schema:  `{"type":"object","properties":{"branch":{"type":"string","default":"main"}},"required":["branch"]}`,
			answers: []string{"Use default"},
			want:    actionAccept,
			content: map[string]any{"branch": "main"},
		},
		{
			name:    "an enum value named Skip is a value, not the sentinel",
			schema:  `{"type":"object","properties":{"next":{"type":"string","enum":["Skip","Retry"]}}}`,
			answers: []string{"Skip"},
			want:    actionAccept,
			content: map[string]any{"next": "Skip"},
		},
		{
			name:    "the reworded sentinel still skips",
			schema:  `{"type":"object","properties":{"next":{"type":"string","enum":["Skip","Retry"]}}}`,
			answers: []string{"Skip this field"},
			want:    actionAccept,
			content: map[string]any{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.params
			if p == nil {
				p = &mcpsdk.ElicitParams{Message: "please fill this in"}
			}
			if tc.schema != "" {
				p.RequestedSchema = schemaOf(t, tc.schema)
			}
			ask := &scriptedAsker{answers: tc.answers}
			el := NewElicitor()
			el.SetAsker(ask)

			got, err := el.elicit(context.Background(), "srv", p)
			if err != nil {
				t.Fatalf("elicit: %v", err)
			}
			if got.Action != tc.want {
				t.Fatalf("action = %q, want %q (asked: %q)", got.Action, tc.want, ask.questions)
			}
			if tc.want != actionAccept {
				if len(got.Content) != 0 {
					t.Errorf("content on a %s = %v, want none", got.Action, got.Content)
				}
				return
			}
			if !reflect.DeepEqual(got.Content, tc.content) {
				t.Errorf("content = %#v, want %#v", got.Content, tc.content)
			}
		})
	}
}

func TestElicitWithoutAskerDeclines(t *testing.T) {
	el := NewElicitor()
	got, err := el.elicit(context.Background(), "srv", &mcpsdk.ElicitParams{Message: "token?"})
	if err != nil {
		t.Fatalf("elicit: %v", err)
	}
	if got.Action != actionDecline {
		t.Errorf("action = %q, want %q", got.Action, actionDecline)
	}
}

func TestNilElicitorAdvertisesNothing(t *testing.T) {
	var el *Elicitor
	if h := el.handlerFor("srv"); h != nil {
		t.Error("a nil Elicitor must not produce a handler, or the capability is advertised with nobody behind it")
	}
	// Must not panic: headless wiring calls SetAsker on the nil value.
	el.SetAsker(&scriptedAsker{})
}

func TestElicitNamesTheServer(t *testing.T) {
	ask := &scriptedAsker{answers: []string{"No"}}
	el := NewElicitor()
	el.SetAsker(ask)
	if _, err := el.elicit(context.Background(), "github", &mcpsdk.ElicitParams{Message: "Grant access?"}); err != nil {
		t.Fatalf("elicit: %v", err)
	}
	if len(ask.questions) != 1 {
		t.Fatalf("asked %d questions, want 1", len(ask.questions))
	}
	q := ask.questions[0]
	if !strings.Contains(q, `"github"`) {
		t.Errorf("question %q does not name the server; the user cannot judge the request without it", q)
	}
	if !strings.Contains(q, "Grant access?") {
		t.Errorf("question %q dropped the server's message", q)
	}
}

func TestElicitAskerErrorPropagates(t *testing.T) {
	want := errors.New("user interrupted")
	el := NewElicitor()
	el.SetAsker(&scriptedAsker{err: want})
	if _, err := el.elicit(context.Background(), "srv", &mcpsdk.ElicitParams{Message: "x"}); !errors.Is(err, want) {
		t.Errorf("err = %v, want %v — a cancelled turn must not read as a user decision", err, want)
	}
}

func TestElicitFieldOrder(t *testing.T) {
	// "required" is a JSON array, so its order survives decoding and is
	// honoured; the optional fields fall back to sorted order because object
	// key order does not survive.
	schema := schemaOf(t, `{
		"type": "object",
		"properties": {
			"zeta":   {"type": "string"},
			"alpha":  {"type": "string"},
			"second": {"type": "string"},
			"first":  {"type": "string"}
		},
		"required": ["second", "first"]
	}`)
	fields, err := elicitFields(schema)
	if err != nil {
		t.Fatalf("elicitFields: %v", err)
	}
	var got []string
	for _, f := range fields {
		got = append(got, f.key)
	}
	want := []string{"second", "first", "alpha", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestElicitOptions(t *testing.T) {
	tests := []struct {
		name  string
		field elicitField
		want  []string
	}{
		{
			name:  "required free text offers only the escape hatch",
			field: elicitField{key: "token", typ: "string", required: true},
			want:  []string{optCancel},
		},
		{
			name:  "optional free text can also be skipped",
			field: elicitField{key: "note", typ: "string"},
			want:  []string{optSkip, optCancel},
		},
		{
			name:  "boolean offers yes and no",
			field: elicitField{key: "force", typ: "boolean", required: true},
			want:  []string{optYes, optNo, optCancel},
		},
		{
			name:  "enum offers its members",
			field: elicitField{key: "env", typ: "string", enum: []string{"dev", "prod"}, required: true},
			want:  []string{"dev", "prod", optCancel},
		},
		{
			name:  "enumNames are what the user sees",
			field: elicitField{key: "env", typ: "string", enum: []string{"dev", "prod"}, enumNames: []string{"Development", "Production"}, required: true},
			want:  []string{"Development", "Production", optCancel},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, o := range tc.field.options() {
				got = append(got, o.Label)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("labels = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestElicitRoundTrip drives a real server session through the SDK, which is
// the only thing that proves the capability is advertised: a handler the client
// never announces is never called, and every unit test above would still pass.
//
// The server asks by returning an InputRequests map rather than by calling
// Elicit — on protocol 2026-07-28 a server may not send elicitation/create
// while it is serving a request (SEP-2322), and the SDK's client middleware is
// what turns that result into a call to our handler and a retry.
func TestElicitRoundTrip(t *testing.T) {
	ctx := context.Background()

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "elicitsrv", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "whoami", Description: "Ask the user who they are"},
		func(_ context.Context, req *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
			if len(req.Params.InputResponses) == 0 {
				return &mcpsdk.CallToolResult{
					RequestState: "asked",
					InputRequests: mcpsdk.InputRequestMap{"who": &mcpsdk.ElicitParams{
						Message: "What name should I use?",
						RequestedSchema: map[string]any{
							"type":       "object",
							"properties": map[string]any{"name": map[string]any{"type": "string"}},
							"required":   []string{"name"},
						},
					}},
				}, nil, nil
			}
			got, ok := req.Params.InputResponses["who"].(*mcpsdk.ElicitResult)
			if !ok {
				return nil, nil, errors.New("no elicit result echoed back")
			}
			return &mcpsdk.CallToolResult{
				Content: []mcpsdk.Content{&mcpsdk.TextContent{
					Text: got.Action + ":" + toString(got.Content["name"]),
				}},
			}, nil, nil
		})

	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	el := NewElicitor()
	el.SetAsker(&scriptedAsker{answers: []string{"Ada"}})
	server, err := ConnectTransport(ctx, "elicitsrv", clientT, el)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	m := &Manager{elicitor: el}
	m.Add(server)
	defer func() { m.Close(); _ = ss.Wait() }()

	var whoami tools.Tool
	for _, tl := range m.Tools(ctx) {
		if strings.HasSuffix(tl.Name(), "__whoami") {
			whoami = tl
		}
	}
	if whoami == nil {
		t.Fatal("whoami tool not registered")
	}
	res, err := whoami.Execute(ctx, tools.Context{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res) != 1 || res[0].IsError {
		t.Fatalf("result = %+v, want one non-error result", res)
	}
	if got := res[0].Content; got != "accept:Ada" {
		t.Errorf("content = %q, want %q", got, "accept:Ada")
	}
}

// TestElicitCapabilityIsFormOnly inspects what the server was actually told at
// initialize. Advertising `elicitation: {form: {}}` is what makes a server fall
// back on its own instead of sending a link a terminal cannot open; a server
// that ignores it still meets the guard in elicit, but one that reads
// capabilities should never get that far.
//
// It also pins the absence of `roots`. Klaudia registers no root, so the SDK's
// default `roots: {listChanged: true}` advertised a feature whose only possible
// answer was an empty list — and roots is deprecated as of 2026-07-28.
func TestElicitCapabilityIsFormOnly(t *testing.T) {
	ctx := context.Background()

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "elicitsrv", Version: "0.0.1"}, nil)
	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	el := NewElicitor()
	server, err := ConnectTransport(ctx, "elicitsrv", clientT, el)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer func() { _ = server.sess().Close(); _ = ss.Wait() }()

	ip := ss.InitializeParams()
	if ip == nil || ip.Capabilities == nil {
		t.Fatal("server saw no client capabilities")
	}
	if ip.ProtocolVersion != "2026-07-28" {
		t.Errorf("negotiated protocol = %q, want 2026-07-28", ip.ProtocolVersion)
	}
	el2 := ip.Capabilities.Elicitation
	if el2 == nil {
		t.Fatal("elicitation capability absent; a server will never ask")
	}
	if el2.Form == nil {
		t.Error("elicitation.form absent, so form elicitation is not advertised")
	}
	if el2.URL != nil {
		t.Error("elicitation.url advertised, but a terminal cannot open a link out of band")
	}
	if ip.Capabilities.Roots.ListChanged {
		t.Error("roots advertised, but Klaudia never registers one")
	}
}

// TestElicitWithoutCapabilityIsNotAsked is the other half: with no Elicitor the
// capability is absent, and a server that asks anyway gets an error rather than
// a silent decline that looks like a user decision.
func TestElicitWithoutCapabilityIsNotAsked(t *testing.T) {
	ctx := context.Background()

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "elicitsrv", Version: "0.0.1"}, nil)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{Name: "whoami"},
		func(_ context.Context, req *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, any, error) {
			if len(req.Params.InputResponses) == 0 {
				return &mcpsdk.CallToolResult{
					RequestState:  "asked",
					InputRequests: mcpsdk.InputRequestMap{"who": &mcpsdk.ElicitParams{Message: "Name?"}},
				}, nil, nil
			}
			return &mcpsdk.CallToolResult{Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "asked anyway"}}}, nil, nil
		})

	clientT, serverT := mcpsdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	server, err := ConnectTransport(ctx, "elicitsrv", clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	m := &Manager{}
	m.Add(server)
	defer func() { m.Close(); _ = ss.Wait() }()

	var whoami tools.Tool
	for _, tl := range m.Tools(ctx) {
		if strings.HasSuffix(tl.Name(), "__whoami") {
			whoami = tl
		}
	}
	if whoami == nil {
		t.Fatal("whoami tool not registered")
	}
	res, err := whoami.Execute(ctx, tools.Context{}, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res) != 1 || !res[0].IsError {
		t.Fatalf("result = %+v, want one error result", res)
	}
	if strings.Contains(res[0].Content, "asked anyway") {
		t.Error("the tool completed, so elicitation was somehow fulfilled without a capability")
	}
}

// TestElicitIsSerialised pins the guard on the SDK fulfilling several input
// requests from one round concurrently (an errgroup, in mrtr.go). The frontend
// has a single question slot: two prompts in flight at once overwrite each
// other's options, and the user answers the wrong question.
func TestElicitIsSerialised(t *testing.T) {
	el := NewElicitor()
	ask := &overlapAsker{}
	el.SetAsker(ask)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = el.elicit(context.Background(), "srv", &mcpsdk.ElicitParams{Message: "ok?"})
		}()
	}
	wg.Wait()
	if n := ask.maxConcurrent.Load(); n != 1 {
		t.Errorf("%d elicitations were in the frontend at once, want 1", n)
	}
}

// overlapAsker records the highest number of simultaneous Ask calls it saw.
type overlapAsker struct {
	inFlight      atomic.Int32
	maxConcurrent atomic.Int32
}

func (a *overlapAsker) Ask(context.Context, string, []tools.AskOption) (string, error) {
	n := a.inFlight.Add(1)
	for {
		m := a.maxConcurrent.Load()
		if n <= m || a.maxConcurrent.CompareAndSwap(m, n) {
			break
		}
	}
	// Long enough that a genuinely concurrent caller is still inside Ask.
	time.Sleep(2 * time.Millisecond)
	a.inFlight.Add(-1)
	return "Yes", nil
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

// The default is stated in the question; a typed field gets a row to accept
// it, while a choice's default is already a row of its own.
func TestElicitShowsTheDefault(t *testing.T) {
	fields, err := elicitFields(schemaOf(t, `{"type":"object","properties":{
		"branch":{"type":"string","default":"main"},
		"env":{"type":"string","oneOf":[{"const":"prd","title":"Production"},{"const":"stg","title":"Staging"}],"default":"stg"}}}`))
	if err != nil || len(fields) != 2 {
		t.Fatalf("fields = %+v, %v", fields, err)
	}
	branch, env := fields[0], fields[1]
	if !strings.Contains(branch.prompt(), "Default: main") || !strings.Contains(env.prompt(), "Default: Staging") {
		t.Errorf("prompts do not state the default:\n%s\n%s", branch.prompt(), env.prompt())
	}
	has := func(opts []tools.AskOption, label string) bool {
		for _, o := range opts {
			if o.Label == label {
				return true
			}
		}
		return false
	}
	if !has(branch.options(), optDefault) {
		t.Error("a typed field with a default offers no row to accept it")
	}
	if has(env.options(), optDefault) {
		t.Error("a choice field duplicates its default as a separate row")
	}
}
