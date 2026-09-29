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

// TestDriverSeedsResumedHistoryAndAnnouncesInit pins the resume contract of the
// embedding channel: the first line is system/init carrying the session id, and
// the first turn runs with the resumed session's history (it used to start
// empty, so a --resume appended to the transcript while the model remembered
// nothing).
func TestDriverSeedsResumedHistoryAndAnnouncesInit(t *testing.T) {
	out := &lineSink{}
	d := NewDriver(out)
	d.SessionID = "sess-7"
	d.History = []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("earlier question")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("earlier answer")}},
	}
	d.Init = &Init{CWD: "/w", Model: "m", PermissionMode: "dontAsk", ResumedFrom: "sess-7"}

	var seen [][]anthropic.BetaMessageParam
	runFn := func(_ context.Context, _ string, history []anthropic.BetaMessageParam, _ agent.Approver, _ agent.Emitter) (agent.Result, error) {
		seen = append(seen, history)
		next := append(append([]anthropic.BetaMessageParam(nil), history...),
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("q")),
			anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("a")}})
		return agent.Result{Text: "a", Messages: next}, nil
	}
	in := strings.NewReader(`{"type":"user","message":{"role":"user","content":"one"}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"two"}}` + "\n")
	if err := d.Run(context.Background(), in, runFn); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || len(seen[0]) != 2 || len(seen[1]) != 4 {
		t.Fatalf("history lengths per turn = %v, want [2 4]", func() []int {
			n := []int{}
			for _, h := range seen {
				n = append(n, len(h))
			}
			return n
		}())
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(out.snapshot()[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first["type"] != "system" || first["subtype"] != "init" || first["session_id"] != "sess-7" ||
		first["resumed"] != true || first["resumed_from"] != "sess-7" || first["history_messages"] != float64(2) {
		t.Fatalf("first line = %v, want system/init for a resumed sess-7", first)
	}
}
