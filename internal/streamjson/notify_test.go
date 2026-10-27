package streamjson

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// A job exit between turns reaches the peer as a system line.
func TestNotifyWritesASystemLine(t *testing.T) {
	var buf bytes.Buffer
	d := NewDriver(&buf)
	d.Notify("job_exit", map[string]any{"job_id": "bash_1", "exit_code": 0})
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("not one JSON line: %q", buf.String())
	}
	if m["type"] != "system" || m["subtype"] != "job_exit" || m["job_id"] != "bash_1" {
		t.Errorf("line = %v", m)
	}
}

// The driver hands each turn its background runner and inbox, so the Agent
// tool's run_in_background works over stream-json instead of falling back to
// the foreground.
func TestDriverPassesBackgroundAndInterject(t *testing.T) {
	var buf bytes.Buffer
	d := NewDriver(&buf)
	d.Background = agent.NewBackground(t.Context(), nil)
	d.Interject = func() agent.Interjection { return agent.Interjection{} }
	var got agent.Turn
	in := strings.NewReader(`{"type":"user","message":{"role":"user","content":"hi"}}` + "\n")
	_ = d.Run(t.Context(), in, nil, func(_ context.Context, turn agent.Turn) (agent.Result, error) {
		got = turn
		return agent.Result{}, nil
	})
	if got.Background == nil || got.Interject == nil {
		t.Errorf("turn Background=%v Interject set=%v", got.Background, got.Interject != nil)
	}
}
