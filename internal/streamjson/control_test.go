package streamjson

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// controlExchange runs one turn and answers the single control_request it makes
// with the given payload, returning every line the driver wrote.
//
// The request id is not knowable in advance, so the answer is written only after
// the request appears on the wire.
func controlExchange(t *testing.T, answer map[string]any, turnFn func(ctx context.Context, turn agent.Turn)) []string {
	t.Helper()
	out := &lineSink{}
	d := NewDriver(out)
	pr, pw := io.Pipe()

	done := make(chan struct{})
	runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
		turnFn(ctx, turn)
		close(done)
		return agent.Result{Text: "ok", NumTurns: 1, StopReason: "end_turn"}, nil
	}

	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"go"}}` + "\n"))
		id := waitForRequestID(out)
		if id == "" {
			_ = pw.Close()
			return
		}
		resp := map[string]any{
			"type":     "control_response",
			"response": map[string]any{"subtype": "success", "request_id": id, "response": answer},
		}
		b, _ := json.Marshal(resp)
		_, _ = pw.Write(append(b, '\n'))
		<-done
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Run(ctx, pr, nil, runFn); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("driver run: %v", err)
	}
	return out.snapshot()
}

// requestPayload returns the decoded "request" object of the first
// control_request line.
func requestPayload(t *testing.T, lines []string) map[string]any {
	t.Helper()
	for _, l := range lines {
		var m struct {
			Type    string         `json:"type"`
			Request map[string]any `json:"request"`
		}
		if json.Unmarshal([]byte(l), &m) == nil && m.Type == "control_request" {
			return m.Request
		}
	}
	t.Fatalf("no control_request in output:\n%s", strings.Join(lines, "\n"))
	return nil
}

// AskUserQuestion was dead over this transport: the CLI passed no Asker because
// there was no wire form for a question, so the model was told nobody could be
// asked on the one channel that always has a user behind it.
func TestAskUserReachesThePeer(t *testing.T) {
	var got string
	var gotErr error
	lines := controlExchange(t, map[string]any{"label": "Rebase"}, func(ctx context.Context, turn agent.Turn) {
		if turn.Asker == nil {
			t.Error("no Asker was supplied to the turn")
			return
		}
		got, gotErr = turn.Asker.Ask(ctx, "Merge or rebase?", []tools.AskOption{
			{Label: "Merge", Description: "keep both histories"},
			{Label: "Rebase"},
		})
	})
	if gotErr != nil {
		t.Fatalf("Ask returned %v", gotErr)
	}
	if got != "Rebase" {
		t.Errorf("Ask = %q, want %q", got, "Rebase")
	}

	req := requestPayload(t, lines)
	if req["subtype"] != "ask_user" {
		t.Errorf("subtype = %v, want ask_user", req["subtype"])
	}
	if req["question"] != "Merge or rebase?" {
		t.Errorf("question = %v", req["question"])
	}
	opts, _ := req["options"].([]any)
	if len(opts) != 2 {
		t.Fatalf("options = %v, want 2 entries", req["options"])
	}
	first, _ := opts[0].(map[string]any)
	if first["label"] != "Merge" || first["description"] != "keep both histories" {
		t.Errorf("first option = %v, want label+description", first)
	}
	// An option with no description must not carry an empty one: the peer
	// renders a blank second line for it.
	second, _ := opts[1].(map[string]any)
	if _, present := second["description"]; present {
		t.Errorf("second option carries an empty description: %v", second)
	}
}

// A peer that has not implemented ask_user must produce an error, not a
// fabricated choice. Returning the first option would report to the model that
// the user picked something they were never shown.
func TestAskUserErrorIsNotAnAnswer(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)
	pr, pw := io.Pipe()

	type result struct {
		label string
		err   error
	}
	resCh := make(chan result, 1)
	runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
		label, err := turn.Asker.Ask(ctx, "pick", []tools.AskOption{{Label: "A"}})
		resCh <- result{label, err}
		return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
	}

	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"go"}}` + "\n"))
		id := waitForRequestID(out)
		b, _ := json.Marshal(map[string]any{
			"type":     "control_response",
			"response": map[string]any{"subtype": "error", "request_id": id, "error": "unsupported"},
		})
		_, _ = pw.Write(append(b, '\n'))
		<-resCh
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = d.Run(ctx, pr, nil, runFn) }()

	select {
	case got := <-resCh:
		if got.err == nil {
			t.Fatalf("Ask succeeded with %q; an unsupported peer must be an error", got.label)
		}
		if got.label != "" {
			t.Errorf("Ask returned a label (%q) alongside its error", got.label)
		}
		if !strings.Contains(got.err.Error(), "unsupported") {
			t.Errorf("error %q does not carry the peer's reason", got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Ask to return")
	}
}

func TestExitPlanReachesThePeer(t *testing.T) {
	tests := []struct {
		name     string
		answer   map[string]any
		approved bool
		wantErr  string
	}{
		{name: "approved", answer: map[string]any{"approved": true}, approved: true},
		{name: "rejected", answer: map[string]any{"approved": false}},
		{
			name:    "rejected with a reason the model should see",
			answer:  map[string]any{"approved": false, "message": "split step 3"},
			wantErr: "split step 3",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var approved bool
			var err error
			lines := controlExchange(t, tc.answer, func(ctx context.Context, turn agent.Turn) {
				if turn.Planner == nil {
					t.Error("no Planner was supplied to the turn")
					return
				}
				approved, err = turn.Planner.ExitPlan(ctx, "1. do this\n2. then that")
			})
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("ExitPlan returned %v", err)
			}
			if approved != tc.approved {
				t.Errorf("approved = %v, want %v", approved, tc.approved)
			}
			req := requestPayload(t, lines)
			if req["subtype"] != "exit_plan" {
				t.Errorf("subtype = %v, want exit_plan", req["subtype"])
			}
			if !strings.Contains(req["plan"].(string), "do this") {
				t.Errorf("plan not forwarded: %v", req["plan"])
			}
		})
	}
}

// A host change used to arrive indistinguishable from an ordinary permission
// ask, so a peer could only render "allow Bash?" for `systemctl restart nginx`.
func TestCanUseToolCarriesTheHostChange(t *testing.T) {
	lines := controlExchange(t, map[string]any{"behavior": "deny"}, func(ctx context.Context, turn agent.Turn) {
		turn.Approver.Approve(ctx, agent.ApprovalRequest{
			ToolName:   "RequestHostChange",
			ToolUseID:  "toolu_42",
			Specifier:  "brew install ripgrep",
			Suggestion: "this installs a package",
			Input:      json.RawMessage(`{"summary":"install ripgrep"}`),
			Detail: &hostgate.HostChange{
				Summary:  "Install ripgrep",
				Reason:   "the project's search needs it",
				Zone:     trust.ZoneHost,
				Packages: []string{"ripgrep"},
				Declared: true,
			},
		})
	})

	req := requestPayload(t, lines)
	if req["subtype"] != "can_use_tool" {
		t.Fatalf("subtype = %v", req["subtype"])
	}
	for field, want := range map[string]any{
		"tool_use_id": "toolu_42",
		"specifier":   "brew install ripgrep",
		"suggestion":  "this installs a package",
	} {
		if req[field] != want {
			t.Errorf("%s = %v, want %v", field, req[field], want)
		}
	}
	hc, ok := req["host_change"].(map[string]any)
	if !ok {
		t.Fatalf("no host_change in the request: %v", req)
	}
	if hc["summary"] != "Install ripgrep" || hc["reason"] != "the project's search needs it" {
		t.Errorf("host_change summary/reason wrong: %v", hc)
	}
	if hc["zone"] != "host" {
		t.Errorf("zone = %v, want host (the string form, not the int)", hc["zone"])
	}
	if hc["declared"] != true {
		t.Errorf("declared = %v, want true", hc["declared"])
	}
	// An empty scope must be absent rather than an empty array, so a peer can
	// tell "no services" from "services it did not say".
	if _, present := hc["services"]; present {
		t.Errorf("empty services list was sent: %v", hc)
	}
}

// An ordinary ask carries none of the host-change framing.
func TestCanUseToolWithoutHostChangeStaysMinimal(t *testing.T) {
	lines := controlExchange(t, map[string]any{"behavior": "allow"}, func(ctx context.Context, turn agent.Turn) {
		turn.Approver.Approve(ctx, agent.ApprovalRequest{
			ToolName: "Bash",
			Input:    json.RawMessage(`{"command":"ls"}`),
		})
	})
	req := requestPayload(t, lines)
	for _, absent := range []string{"host_change", "tool_use_id", "specifier", "suggestion"} {
		if _, present := req[absent]; present {
			t.Errorf("%s should be absent for a plain ask: %v", absent, req)
		}
	}
}

// --resume resolved a transcript and then threw it away on this path: the CLI
// reported a resumed session while the agent started from nothing.
func TestResumedHistoryReachesTheFirstTurn(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)
	pr, pw := io.Pipe()

	history := []anthropic.BetaMessageParam{{
		Role:    anthropic.BetaMessageParamRoleUser,
		Content: []anthropic.BetaContentBlockParamUnion{{OfText: &anthropic.BetaTextBlockParam{Text: "earlier"}}},
	}}

	seen := make(chan int, 2)
	runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
		seen <- len(turn.History)
		// Two messages back means the second turn continues this one.
		return agent.Result{
			Text:     "ok",
			Messages: append(append([]anthropic.BetaMessageParam(nil), turn.History...), history[0]),
		}, nil
	}

	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"one"}}` + "\n"))
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"two"}}` + "\n"))
		time.Sleep(200 * time.Millisecond)
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Run(ctx, pr, history, runFn); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("driver run: %v", err)
	}

	if n := <-seen; n != 1 {
		t.Errorf("first turn saw %d history messages, want the 1 resumed one", n)
	}
	if n := <-seen; n != 2 {
		t.Errorf("second turn saw %d history messages, want 2 (resumed + the first turn)", n)
	}
}

func TestResultEventSubtype(t *testing.T) {
	tests := []struct {
		name        string
		res         agent.Result
		err         error
		wantSubtype string
		wantError   bool
		resultHas   string
	}{
		{
			name:        "an ordinary answer succeeds",
			res:         agent.Result{Text: "here you go", StopReason: "end_turn"},
			wantSubtype: "success",
			resultHas:   "here you go",
		},
		{
			// Reporting success with an empty result let a pipeline treat a
			// refusal as a finished task. The headless path already fixed this;
			// this one had not inherited it.
			name:        "a refusal is not a success",
			res:         agent.Result{Text: "", StopReason: "refusal"},
			wantSubtype: "refusal",
			wantError:   true,
			resultHas:   "declined",
		},
		{
			name:        "silence at the output limit is not a success",
			res:         agent.Result{Text: "", StopReason: "max_tokens"},
			wantSubtype: "max_tokens",
			wantError:   true,
			resultHas:   "output limit",
		},
		{
			// Partial text is a real answer, cut short. The stop reason already
			// says so and the text is worth keeping as the result.
			name:        "a truncated answer keeps its text",
			res:         agent.Result{Text: "as far as I got", StopReason: "max_tokens"},
			wantSubtype: "success",
			resultHas:   "as far as I got",
		},
		{
			name:        "an empty end_turn has nothing to explain",
			res:         agent.Result{Text: "", StopReason: "end_turn"},
			wantSubtype: "success",
		},
		{
			name:        "an error reports as one",
			res:         agent.Result{},
			err:         errors.New("boom"),
			wantSubtype: "error_during_execution",
			wantError:   true,
			resultHas:   "boom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := resultEvent(tc.res, tc.err)
			if m["subtype"] != tc.wantSubtype {
				t.Errorf("subtype = %v, want %v", m["subtype"], tc.wantSubtype)
			}
			if m["is_error"] != tc.wantError {
				t.Errorf("is_error = %v, want %v", m["is_error"], tc.wantError)
			}
			if tc.resultHas != "" && !strings.Contains(m["result"].(string), tc.resultHas) {
				t.Errorf("result %q does not mention %q", m["result"], tc.resultHas)
			}
		})
	}
}
