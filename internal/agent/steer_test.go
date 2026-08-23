package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// The point of the whole mechanism: "don't modify the API" has to reach the
// model before it modifies the API, not after.
func TestInterjectionLandsBeforeTheNextRequest(t *testing.T) {
	var mu sync.Mutex
	pending := Interjection{Text: "don't modify the API"}
	taken := 0
	opts := Options{Interject: func() Interjection {
		mu.Lock()
		defer mu.Unlock()
		in := pending
		if !in.Empty() {
			taken++
		}
		pending = Interjection{}
		return in
	}}

	in := pollInterjection(opts)
	if in.Text != "don't modify the API" {
		t.Fatalf("poll returned %+v", in)
	}
	msg, ok := steerMessage(in)
	if !ok {
		t.Fatal("an interjection produced no message")
	}
	body := messageText(msg)
	if !strings.Contains(body, "don't modify the API") {
		t.Errorf("the instruction is not in the message: %q", body)
	}
	// Framing matters: without it the model treats a mid-task aside as
	// conversation and acknowledges it instead of obeying it.
	if !strings.Contains(body, "applies from now on") {
		t.Errorf("the message does not frame this as a live instruction: %q", body)
	}

	// A second poll finds nothing: draining is what stops the same correction
	// being appended on every subsequent turn.
	if !pollInterjection(opts).Empty() {
		t.Error("the interjection was not consumed")
	}
	if taken != 1 {
		t.Errorf("consumed %d times, want 1", taken)
	}
}

// "Stop after this test run" must not throw away the test run.
func TestHaltAsksForAWrapUpRatherThanSilence(t *testing.T) {
	msg, ok := steerMessage(Interjection{Halt: true})
	if !ok {
		t.Fatal("halt produced no message")
	}
	body := messageText(msg)
	for _, want := range []string{"stop after the current step", "Summarise what you completed", "what remains"} {
		if !strings.Contains(body, want) {
			t.Errorf("halt message missing %q: %q", want, body)
		}
	}
	if strings.Contains(strings.ToLower(body), "discard") {
		t.Errorf("halt should not ask the model to throw work away: %q", body)
	}
}

// Text and halt together: "finish this, then stop" is one action, not two.
func TestTextAndHaltCombine(t *testing.T) {
	msg, ok := steerMessage(Interjection{Text: "only investigate", Halt: true})
	if !ok {
		t.Fatal("no message")
	}
	body := messageText(msg)
	if !strings.Contains(body, "only investigate") || !strings.Contains(body, "stop after the current step") {
		t.Errorf("combined message lost half of itself: %q", body)
	}
}

func TestEmptyInterjectionProducesNothing(t *testing.T) {
	if !(Interjection{}).Empty() {
		t.Error("the zero interjection is not empty")
	}
	if !(Interjection{Text: "   "}).Empty() {
		t.Error("whitespace is not empty")
	}
	if (Interjection{Halt: true}).Empty() {
		t.Error("a halt with no text is still something to act on")
	}
	if _, ok := steerMessage(Interjection{}); ok {
		t.Error("an empty interjection produced a message")
	}
}

// A nil Interject is the headless case and must not panic or invent anything.
func TestNoInterjectSourceIsInert(t *testing.T) {
	if !pollInterjection(Options{}).Empty() {
		t.Error("a run with no interjection source produced one")
	}
}

func messageText(m anthropic.BetaMessageParam) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// recordingProvider captures the messages sent on each request, so a test can
// prove *when* an interjection reached the model rather than only that it did.
type recordingProvider struct {
	turns    []anthropic.BetaMessage
	n        int
	requests [][]anthropic.BetaMessageParam
}

func (p *recordingProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.requests = append(p.requests, params.Messages)
	if p.n >= len(p.turns) {
		return anthropic.BetaMessage{StopReason: "end_turn"}, nil
	}
	m := p.turns[p.n]
	p.n++
	return m, nil
}

// The claim this stage exists to make: a correction typed while a tool is
// running is in the *next* request, before the next tool is dispatched.
func TestInterjectionReachesTheModelBeforeTheNextToolDispatch(t *testing.T) {
	var dispatched []string
	rec := &recordingTool{onExec: func(cmd string) { dispatched = append(dispatched, cmd) }}
	reg := tools.NewRegistry(rec)

	provider := &recordingProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "t1", "Recorder", map[string]any{"note": "first"}),
		toolUseTurn(t, "t2", "Recorder", map[string]any{"note": "second"}),
	}}

	// The user types during the first tool batch: available from the second
	// poll onward, which is the poll that happens after that batch.
	polls := 0
	interject := func() Interjection {
		polls++
		if polls == 2 {
			return Interjection{Text: "don't touch the API"}
		}
		return Interjection{}
	}

	l := New(provider, reg)
	if _, err := l.Run(context.Background(), Options{
		Prompt: "go", Permission: bypassPerm(), Interject: interject,
	}, nil); err != nil {
		t.Fatal(err)
	}

	if len(provider.requests) < 2 {
		t.Fatalf("only %d requests were made", len(provider.requests))
	}
	// The second request must already carry the instruction, and the second
	// tool must not have run before it was sent.
	second := marshalMessages(t, provider.requests[1])
	if !strings.Contains(second, "don't touch the API") {
		t.Fatalf("the correction was not in the request that decided the next action:\n%s", second)
	}
	firstReq := marshalMessages(t, provider.requests[0])
	if strings.Contains(firstReq, "don't touch the API") {
		t.Error("the correction appeared in a request sent before the user typed it")
	}
	if len(dispatched) != 2 || dispatched[0] != "first" {
		t.Errorf("dispatched = %v", dispatched)
	}
}

// Halt ends the run and reports, rather than letting the model carry on with a
// polite acknowledgement.
func TestHaltStopsTheLoop(t *testing.T) {
	rec := &recordingTool{}
	reg := tools.NewRegistry(rec)
	provider := &recordingProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "t1", "Recorder", map[string]any{"note": "one"}),
		toolUseTurn(t, "t2", "Recorder", map[string]any{"note": "two"}),
		toolUseTurn(t, "t3", "Recorder", map[string]any{"note": "three"}),
	}}
	polls := 0
	l := New(provider, reg)
	res, err := l.Run(context.Background(), Options{
		Prompt: "go", Permission: bypassPerm(),
		Interject: func() Interjection {
			polls++
			if polls == 2 {
				return Interjection{Halt: true}
			}
			return Interjection{}
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.StopReason != "user_halt" {
		t.Errorf("stop reason = %q, want user_halt", res.StopReason)
	}
	// The first tool ran; the third must not have.
	if len(rec.calls) > 2 {
		t.Errorf("the loop kept going after a halt: %v", rec.calls)
	}
	if len(rec.calls) == 0 {
		t.Error("halt discarded the step that was already in flight")
	}
}

// recordingTool notes what it was asked to do.
type recordingTool struct {
	calls  []string
	onExec func(string)
}

func (recordingTool) Name() string                                { return "Recorder" }
func (recordingTool) Description(context.Context) (string, error) { return "records", nil }
func (recordingTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"note":{"type":"string"}}}`)
}
func (recordingTool) ValidateInput(json.RawMessage) error { return nil }
func (recordingTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (recordingTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (r *recordingTool) Execute(_ context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	var in struct {
		Note string `json:"note"`
	}
	_ = json.Unmarshal(raw, &in)
	r.calls = append(r.calls, in.Note)
	if r.onExec != nil {
		r.onExec(in.Note)
	}
	return []tools.Result{{Content: "ok"}}, nil
}

func marshalMessages(t *testing.T, msgs []anthropic.BetaMessageParam) string {
	t.Helper()
	b, err := json.Marshal(msgs)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
