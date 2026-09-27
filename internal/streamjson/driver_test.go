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

// TestResultLineCarriesUsage pins that the embedding channel's result line
// reports the turn's token usage, matching the -p ResultMessage shape. An
// embedder that only reads the result (rather than summing the per-call
// "usage" deltas) must still see the real counts, not zeros.
func TestResultLineCarriesUsage(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)

	runFn := func(_ context.Context, _ string, _ []anthropic.BetaMessageParam, _ agent.Approver, emit agent.Emitter) (agent.Result, error) {
		emit(agent.Event{Type: "usage", InputDelta: 1200, OutputDelta: 40, TurnDelta: 1})
		emit(agent.Event{Type: "usage", InputDelta: 300, OutputDelta: 5, TurnDelta: 1})
		return agent.Result{
			Text: "done", NumTurns: 2, StopReason: "end_turn",
			InputTokens: 1500, OutputTokens: 45, CacheReadInputTokens: 7,
		}, nil
	}

	in := strings.NewReader(`{"type":"user","message":{"role":"user","content":"hi"}}` + "\n")
	if err := d.Run(context.Background(), in, runFn); err != nil {
		t.Fatalf("driver run: %v", err)
	}

	var result map[string]any
	for _, l := range out.snapshot() {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not JSON: %s", l)
		}
		if m["type"] == "result" {
			result = m
		}
	}
	if result == nil {
		t.Fatalf("no result line:\n%s", strings.Join(out.snapshot(), "\n"))
	}
	usage, ok := result["usage"].(map[string]any)
	if !ok {
		t.Fatalf("result line has no usage object: %v", result)
	}
	if usage["input_tokens"] != float64(1500) || usage["output_tokens"] != float64(45) {
		t.Errorf("usage = %v, want input_tokens=1500 output_tokens=45", usage)
	}
	if usage["cache_read_input_tokens"] != float64(7) || usage["cache_creation_input_tokens"] != float64(0) {
		t.Errorf("cache usage = %v, want read=7 creation=0", usage)
	}
}
