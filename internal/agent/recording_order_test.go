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

// captureRecorder collects every Record call in order — to assert that the
// transcript only sees the assistant turn AFTER its tool_result is also ready
// (so a process-death window during dispatch can't leave an orphan).
type captureRecorder struct {
	mu   sync.Mutex
	rows []string
}

func (r *captureRecorder) Record(role string, msg json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, role)
	return nil
}

func (r *captureRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.rows...)
}

// scriptedProvider returns each preset message in turn, then synthesises an
// empty end_turn so the loop exits cleanly. Implements api.Provider.
type scriptedProvider struct {
	turns []anthropic.BetaMessage
	n     int
}

func (p *scriptedProvider) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	if p.n >= len(p.turns) {
		return anthropic.BetaMessage{StopReason: "end_turn"}, nil
	}
	m := p.turns[p.n]
	p.n++
	return m, nil
}

// markerTool stamps the captureRecorder during Execute so the test can prove
// where dispatch lands relative to the assistant/user records.
type markerTool struct{ rec *captureRecorder }

func (markerTool) Name() string                                { return "Marker" }
func (markerTool) Description(context.Context) (string, error) { return "", nil }
func (markerTool) InputSchema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (markerTool) ValidateInput(json.RawMessage) error         { return nil }
func (markerTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (markerTool) CheckPermissions(_ permission.Context, _ permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (m markerTool) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	_ = m.rec.Record("DISPATCH", nil)
	return []tools.Result{{Content: "ok"}}, nil
}

// TestAssistantToolUseRecordedAfterDispatch verifies the recording order fix:
// when an assistant turn issues a tool_use, the assistant must not be persisted
// to the transcript until its tool dispatch has completed (i.e. the
// tool_result is ready to be persisted in the next record call). Before the
// fix, dispatch's full duration was the window in which a process kill could
// leak an orphan assistant tool_use to disk — exactly what happened in the
// huedoku transcript at entry 439.
//
// We assert recording order rather than simulating process death: the marker
// tool stamps a "DISPATCH" record during Execute, and we require that record
// to land BEFORE the assistant record.
func TestAssistantToolUseRecordedAfterDispatch(t *testing.T) {
	// Build the assistant turn via JSON so the SDK populates its internal raw
	// fields (manual struct literals don't, and dispatch can't read the name).
	asstJSON := `{
		"role": "assistant",
		"stop_reason": "tool_use",
		"content": [{"type": "tool_use", "id": "tu_1", "name": "Marker", "input": {}}]
	}`
	var asst anthropic.BetaMessage
	if err := json.Unmarshal([]byte(asstJSON), &asst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	rec := &captureRecorder{}
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{asst}}
	loop := New(provider, tools.NewRegistry(markerTool{rec: rec}))

	if _, err := loop.Run(context.Background(), Options{Recorder: rec, MaxTurns: 1}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rows := rec.snapshot()
	if len(rows) < 3 {
		t.Fatalf("want at least DISPATCH, assistant, user; got %v", rows)
	}
	// The exact invariant: the dispatch marker records BEFORE the assistant.
	// If anything kills the process between dispatch and the back-to-back
	// assistant+user records, the assistant is not on disk yet — no orphan.
	var dispatchIdx, assistantIdx int = -1, -1
	for i, r := range rows {
		if r == "DISPATCH" && dispatchIdx < 0 {
			dispatchIdx = i
		}
		if r == "assistant" && assistantIdx < 0 {
			assistantIdx = i
		}
	}
	if dispatchIdx < 0 || assistantIdx < 0 || dispatchIdx >= assistantIdx {
		t.Errorf("dispatch must record before assistant; order = %v (dispatch=%d, assistant=%d)",
			rows, dispatchIdx, assistantIdx)
	}
	// And the assistant should be immediately followed by its paired user.
	if assistantIdx+1 >= len(rows) || rows[assistantIdx+1] != "user" {
		t.Errorf("assistant must be paired with user back-to-back; got rows=%v", rows)
	}
	// Sanity: no leaked stray entries (dispatch + assistant + user with tools,
	// then final assistant for the empty end_turn).
	if !strings.Contains(strings.Join(rows, ","), "DISPATCH,assistant,user") {
		t.Errorf("expected DISPATCH,assistant,user contiguous; got %v", rows)
	}
}
