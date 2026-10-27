package streamjson

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// The driver writes agent.Event verbatim, so the plan payload's wire shape is
// whatever plan.Snapshot marshals to. Untagged, it went out as
// {"Items":[{"Content":...,"ActiveForm":...}],"Rev":1} — PascalCase beside the
// snake_case of every other field — and a cleared plan as "Items":null, which
// a client cannot tell from "no plan field". Pin the shape a client codes to.
func TestPlanEventWireShape(t *testing.T) {
	tests := []struct {
		name  string
		clear bool // replace the steps with nothing after setting them
		want  string
	}{
		{
			name: "steps in snake_case",
			want: `{"items":[
				{"content":"write it","status":"in_progress","active_form":"Writing it"},
				{"content":"test it","status":"pending","active_form":"Testing it"}
			],"approved":false,"rev":1}`,
		},
		{
			name:  "a cleared plan is an empty array",
			clear: true,
			want:  `{"items":[],"approved":false,"rev":2}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := plan.New("")
			steps := []plan.Item{
				{Content: "write it", Status: plan.StatusInProgress, ActiveForm: "Writing it"},
				{Content: "test it", Status: plan.StatusPending, ActiveForm: "Testing it"},
			}
			if err := s.Replace(steps); err != nil {
				t.Fatal(err)
			}
			if tc.clear {
				if err := s.Replace(nil); err != nil {
					t.Fatal(err)
				}
			}
			snap := s.Snapshot()

			got := planLine(t, func(turn agent.Turn) {
				turn.Emit(agent.Event{Type: "plan", Plan: &snap})
			})
			var gotV, wantV any
			if err := json.Unmarshal(got, &gotV); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &wantV); err != nil {
				t.Fatal(err)
			}
			g, _ := json.Marshal(gotV)
			w, _ := json.Marshal(wantV)
			if string(g) != string(w) {
				t.Errorf("plan payload =\n%s\nwant\n%s", g, w)
			}
		})
	}
}

// planLine runs one turn that calls emit and returns the "plan" field of the
// plan event the driver wrote.
func planLine(t *testing.T, emit func(agent.Turn)) json.RawMessage {
	t.Helper()
	out := &lineSink{}
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte(`{"type":"user","message":{"role":"user","content":"go"}}` + "\n"))
		_ = pw.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = NewDriver(out).Run(ctx, pr, nil, func(_ context.Context, turn agent.Turn) (agent.Result, error) {
		emit(turn)
		return agent.Result{Text: "ok", StopReason: "end_turn"}, nil
	})
	for _, l := range out.snapshot() {
		var m struct {
			Type string          `json:"type"`
			Plan json.RawMessage `json:"plan"`
		}
		if json.Unmarshal([]byte(l), &m) == nil && m.Type == "plan" {
			return m.Plan
		}
	}
	t.Fatalf("no plan event written:\n%s", strings.Join(out.snapshot(), "\n"))
	return nil
}
