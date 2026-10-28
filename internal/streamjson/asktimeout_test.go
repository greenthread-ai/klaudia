package streamjson

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// A peer that never answers can_use_tool (it never implemented the control
// protocol, or it stalled) used to wedge the turn forever. The wait is now
// bounded and the ask resolves to a deny that says why. (PR #7.)
func TestDriverUnansweredAskTimesOutAsDeny(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)
	d.AskTimeout = 50 * time.Millisecond

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	var dec permission.Decision
	var askErr error
	turnDone := make(chan struct{})
	runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
		dec = turn.Approver.Approve(ctx, agent.ApprovalRequest{ToolName: "Bash", Input: json.RawMessage(`{}`)})
		_, askErr = turn.Asker.Ask(ctx, "which?", nil)
		close(turnDone)
		return agent.Result{Text: "done:" + string(dec.Behavior), NumTurns: 1, StopReason: "end_turn"}, nil
	}
	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"go"}}` + "\n"))
		<-turnDone // never answer; close stdin once the turn ends
		_ = pw.Close()
	}()

	// Far beyond the ask timeout: a driver that still blocked fails here.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := d.Run(ctx, pr, nil, runFn); err != nil {
		t.Fatalf("driver run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("driver took %s; the ask did not time out", elapsed)
	}
	if dec.Behavior != permission.Deny {
		t.Fatalf("behavior = %q, want deny", dec.Behavior)
	}
	for _, want := range []string{"can_use_tool", "no control_response", "50ms"} {
		if !strings.Contains(dec.Message, want) {
			t.Errorf("deny message missing %q: %s", want, dec.Message)
		}
	}
	if askErr == nil || !strings.Contains(askErr.Error(), "ask_user") {
		t.Errorf("ask_user error = %v, want a timeout naming the request", askErr)
	}
}

// An answer that arrives after its ask was denied is dropped, not applied to
// a later request.
func TestDriverLateAnswerAfterTimeoutIsDropped(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)
	d.AskTimeout = 20 * time.Millisecond

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	decisions := make(chan permission.Decision, 2)
	runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
		dec := turn.Approver.Approve(ctx, agent.ApprovalRequest{ToolName: "Bash", Input: json.RawMessage(`{}`)})
		decisions <- dec
		return agent.Result{Text: "done:" + string(dec.Behavior), NumTurns: 1, StopReason: "end_turn"}, nil
	}
	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"first"}}` + "\n"))
		id := waitForRequestID(out)
		<-decisions // the first ask has timed out
		b, _ := json.Marshal(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype": "success", "request_id": id,
				"response": map[string]any{"behavior": "allow"},
			},
		})
		_, _ = pw.Write(append(b, '\n'))
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"second"}}` + "\n"))
		<-decisions
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Run(ctx, pr, nil, runFn); err != nil {
		t.Fatalf("driver run: %v", err)
	}
	joined := strings.Join(out.snapshot(), "\n")
	if strings.Contains(joined, `"result":"done:allow"`) {
		t.Errorf("a late allow was applied to a turn:\n%s", joined)
	}
	if n := strings.Count(joined, `"result":"done:deny"`); n != 2 {
		t.Errorf("want 2 denied turns, got %d:\n%s", n, joined)
	}
}
