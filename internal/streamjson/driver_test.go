package streamjson

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
	"github.com/greenthread-ai/klaudia/internal/permission"
)

// lineSink captures emitted JSON lines, safe for concurrent writes/reads.
type lineSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *lineSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if l != "" {
			s.lines = append(s.lines, l)
		}
	}
	return len(p), nil
}

func (s *lineSink) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

func TestDriverPermissionRoundTrip(t *testing.T) {
	// Input: a user message, then (once the agent asks) an allow response.
	// We can't know the request_id in advance, so the RunFunc captures the
	// approver and we feed the response after observing the control_request.
	out := &lineSink{}
	d := NewDriver(out)

	// A pipe lets the test write control responses after the request appears.
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	decisionCh := make(chan permission.Decision, 1)
	runFn := func(ctx context.Context, prompt string, _ []anthropic.BetaMessageParam, ap agent.Approver, emit agent.Emitter) (agent.Result, error) {
		emit(agent.Event{Type: "assistant", Text: "working"})
		dec := ap.Approve(ctx, agent.ApprovalRequest{ToolName: "Bash", Input: json.RawMessage(`{"command":"ls"}`)})
		decisionCh <- dec
		return agent.Result{Text: "done:" + string(dec.Behavior), NumTurns: 1, StopReason: "end_turn"}, nil
	}

	// Feed a user message, then watch out for the control_request and answer it.
	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"run ls"}}` + "\n"))
		// Wait for the control_request to be emitted, then grab its id and allow.
		id := waitForRequestID(out)
		resp := map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": id,
				"response":   map[string]any{"behavior": "allow"},
			},
		}
		b, _ := json.Marshal(resp)
		_, _ = pw.Write(append(b, '\n'))
		// Give the turn a moment to finish, then close stdin to end the driver.
		<-decisionCh
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Run(ctx, pr, runFn); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("driver run: %v", err)
	}

	joined := strings.Join(out.snapshot(), "\n")
	if !strings.Contains(joined, `"type":"control_request"`) {
		t.Errorf("expected a control_request in output:\n%s", joined)
	}
	if !strings.Contains(joined, `"result":"done:allow"`) {
		t.Errorf("expected result reflecting the allow decision:\n%s", joined)
	}
}

// waitForRequestID polls the sink until a control_request line appears and
// returns its request_id.
func waitForRequestID(out *lineSink) string {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range out.snapshot() {
			var m struct {
				Type      string `json:"type"`
				RequestID string `json:"request_id"`
			}
			if json.Unmarshal([]byte(l), &m) == nil && m.Type == "control_request" {
				return m.RequestID
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ""
}

// TestDriverUnansweredAskTimesOutAsDeny pins the fix for a peer that never
// answers can_use_tool: before, Approve selected only on ctx.Done() and the
// answer channel, so an embedder that relied on the config allow list (or did
// not implement the control protocol at all) wedged the agent mid-turn
// forever. Now the wait is bounded and the ask resolves to a deny whose message
// tells the model — and, through the tool_result, the peer — what happened.
func TestDriverUnansweredAskTimesOutAsDeny(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)
	d.AskTimeout = 50 * time.Millisecond

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	var dec permission.Decision
	turnDone := make(chan struct{})
	runFn := func(ctx context.Context, _ string, _ []anthropic.BetaMessageParam, ap agent.Approver, _ agent.Emitter) (agent.Result, error) {
		dec = ap.Approve(ctx, agent.ApprovalRequest{ToolName: "mcp__loki__loki_query", Input: json.RawMessage(`{}`)})
		close(turnDone)
		return agent.Result{Text: "done:" + string(dec.Behavior), NumTurns: 1, StopReason: "end_turn"}, nil
	}

	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"query loki"}}` + "\n"))
		// Never answer the control_request; just close stdin once the turn ends.
		<-turnDone
		_ = pw.Close()
	}()

	// Well beyond the ask timeout: if the driver still blocked, this is what
	// would fail the test rather than the assertions below.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if err := d.Run(ctx, pr, runFn); err != nil {
		t.Fatalf("driver run: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("driver took %s; the ask did not time out", elapsed)
	}

	if dec.Behavior != permission.Deny {
		t.Fatalf("behavior = %q, want deny", dec.Behavior)
	}
	for _, want := range []string{"mcp__loki__loki_query", "no control_response", "50ms", "[permissions] allow"} {
		if !strings.Contains(dec.Message, want) {
			t.Errorf("deny message missing %q:\n%s", want, dec.Message)
		}
	}
	joined := strings.Join(out.snapshot(), "\n")
	if !strings.Contains(joined, `"type":"control_request"`) {
		t.Errorf("expected the control_request to have been emitted:\n%s", joined)
	}
	if !strings.Contains(joined, `"result":"done:deny"`) {
		t.Errorf("expected result reflecting the timed-out deny:\n%s", joined)
	}
}

// TestDriverLateAnswerAfterTimeoutIsDropped: an answer that arrives after the
// ask has already been denied must be discarded, not delivered to a waiter that
// is gone or applied to a later request.
func TestDriverLateAnswerAfterTimeoutIsDropped(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)
	d.AskTimeout = 20 * time.Millisecond

	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()

	decisions := make(chan permission.Decision, 2)
	runFn := func(ctx context.Context, prompt string, _ []anthropic.BetaMessageParam, ap agent.Approver, _ agent.Emitter) (agent.Result, error) {
		dec := ap.Approve(ctx, agent.ApprovalRequest{ToolName: "Bash", Input: json.RawMessage(`{"command":"` + prompt + `"}`)})
		decisions <- dec
		return agent.Result{Text: "done:" + string(dec.Behavior), NumTurns: 1, StopReason: "end_turn"}, nil
	}

	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"first"}}` + "\n"))
		id := waitForRequestID(out)
		<-decisions // first ask has timed out and been denied
		// Answer it anyway — late — with an allow.
		b, _ := json.Marshal(map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype": "success", "request_id": id,
				"response": map[string]any{"behavior": "allow"},
			},
		})
		_, _ = pw.Write(append(b, '\n'))
		// A second turn asks again and is also left unanswered; the stale allow
		// must not leak into it.
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"second"}}` + "\n"))
		<-decisions
		_ = pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Run(ctx, pr, runFn); err != nil {
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
