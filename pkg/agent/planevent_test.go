package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// toolTurn builds an assistant message calling one tool. Via JSON so the SDK
// populates the union's raw fields; a struct literal leaves dispatch unable to
// read the tool name.
func toolTurn(t *testing.T, id, name, input string) anthropic.BetaMessage {
	t.Helper()
	var m anthropic.BetaMessage
	raw := `{"role":"assistant","stop_reason":"tool_use","content":[{"type":"tool_use","id":"` + id +
		`","name":"` + name + `","input":` + input + `}]}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// The TUI has never shown the plan, because nothing told it the plan existed:
// the store was written by TodoWrite and read by no frontend. These pin the
// event that now does — when it fires, where it lands, and when it stays quiet.
func TestPlanEventFollowsTheChangeThatCausedIt(t *testing.T) {
	todo := `{"todos":[{"content":"a","status":"in_progress","activeForm":"Doing a"},{"content":"b","status":"pending","activeForm":"Doing b"}]}`
	tests := []struct {
		name  string
		store bool
		// maxTurns stops the run after that many requests; 0 is unlimited.
		maxTurns int
		// replies are the model's turns, in order; an empty reply ends the run.
		replies []anthropic.BetaMessage
		// want is the event types of interest, in order.
		want []string
	}{
		{
			name:    "a TodoWrite is followed by the plan it produced",
			store:   true,
			replies: []anthropic.BetaMessage{toolTurn(t, "t1", "TodoWrite", todo)},
			want:    []string{"tool_use", "tool_result", "plan"},
		},
		{
			// A plan event after every tool call would redraw the panel on
			// every Read, and would make ACP post an identical plan update per
			// tool call.
			name:    "a batch that does not touch the plan emits none",
			store:   true,
			replies: []anthropic.BetaMessage{toolTurn(t, "t1", "Flip", `{}`)},
			want:    []string{"tool_use", "tool_result"},
		},
		{
			name:  "two changes, two events",
			store: true,
			replies: []anthropic.BetaMessage{
				toolTurn(t, "t1", "TodoWrite", todo),
				toolTurn(t, "t2", "Flip", `{}`),
				toolTurn(t, "t3", "TodoWrite", todo),
			},
			want: []string{"tool_use", "tool_result", "plan", "tool_use", "tool_result", "tool_use", "tool_result", "plan"},
		},
		{
			// The run stops straight after the batch, so there is no next
			// turn to report it; without the exit check the panel shows the
			// previous step until the user types again.
			name:     "a run cut off by MaxTurns still reports its last change",
			store:    true,
			maxTurns: 1,
			replies:  []anthropic.BetaMessage{toolTurn(t, "t1", "TodoWrite", todo)},
			want:     []string{"tool_use", "tool_result", "plan"},
		},
		{
			// Sub-agents run with no store; nothing may be emitted for them.
			name:    "no store, no events",
			replies: []anthropic.BetaMessage{toolTurn(t, "t1", "Flip", `{}`)},
			want:    []string{"tool_use", "tool_result"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var store *plan.Store
			if tc.store {
				store = plan.New("")
			}
			tw, err := tools.NewTodoWrite(store)
			if err != nil {
				t.Fatal(err)
			}
			reg := tools.NewRegistry(tw, flipTool{onRun: func() {}})
			l := New(&msgCapture{reply: tc.replies}, reg)
			var got []string
			var last *plan.Snapshot
			emit := func(ev Event) {
				switch ev.Type {
				case "tool_use", "tool_result", "plan":
					got = append(got, ev.Type)
				}
				if ev.Type == "plan" {
					last = ev.Plan
				}
			}
			opts := Options{Prompt: "go", Model: "test", MaxTurns: tc.maxTurns, Permission: permission.Context{}}
			if store != nil {
				opts.Plan = store
			}
			if _, err := l.Run(context.Background(), opts, emit); err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("events = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("events = %v, want %v", got, tc.want)
				}
			}
			if last != nil {
				if a, ok := last.Active(); !ok || a.ActiveForm != "Doing a" {
					t.Errorf("plan event carried %+v, want the in-progress step", last.Items)
				}
			}
		})
	}
}

// A TUI turn is a Run, so the tracker starts over each turn. The current plan
// is re-sent once at the start of the next one — which is how a frontend that
// was not listening (ACP after session/load) catches up — and only once.
func TestTheNextTurnResendsThePlanOnce(t *testing.T) {
	store := plan.New("")
	_ = store.Replace([]plan.Item{{Content: "a", Status: plan.StatusPending}})
	reg := tools.NewRegistry(flipTool{onRun: func() {}})
	l := New(&msgCapture{reply: []anthropic.BetaMessage{toolTurn(t, "t1", "Flip", `{}`)}}, reg)
	n := 0
	emit := func(ev Event) {
		if ev.Type == "plan" {
			n++
		}
	}
	if _, err := l.Run(context.Background(), Options{Prompt: "go", Model: "test", Plan: store}, emit); err != nil {
		t.Fatalf("run: %v", err)
	}
	if n != 1 {
		t.Errorf("plan events = %d, want 1", n)
	}
}

// A resumed session's plan is told to the model on its first prompt, and only
// that one. Measured before: the panel showed the reloaded plan while the
// model, asked, said there was none.
func TestReloadedPlanIsToldOnTheFirstPrompt(t *testing.T) {
	path := plan.PathFor(t.TempDir(), "sess")
	made := plan.New(path)
	_ = made.Approve("Ship it.")
	_ = made.Replace([]plan.Item{{Content: "build it", Status: plan.StatusInProgress}})
	store := plan.Open(path)

	lastUserText := func(msgs []anthropic.BetaMessageParam) string {
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == anthropic.BetaMessageParamRoleUser {
				var b strings.Builder
				for _, c := range msgs[i].Content {
					if c.OfText != nil {
						b.WriteString(c.OfText.Text)
					}
				}
				return b.String()
			}
		}
		return ""
	}
	end := func() anthropic.BetaMessage {
		var m anthropic.BetaMessage
		_ = json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`), &m)
		return m
	}
	for i, want := range []bool{true, false} {
		p := &droppingProvider{turns: []anthropic.BetaMessage{end()}}
		if _, err := New(p, tools.NewRegistry()).Run(context.Background(), Options{Prompt: "carry on", Model: "test", Plan: store}, nil); err != nil {
			t.Fatal(err)
		}
		got := lastUserText(p.sent[0].Messages)
		if told := strings.Contains(got, "build it _(in progress)_"); told != want {
			t.Errorf("turn %d: plan told = %v, want %v\n%s", i+1, told, want, got)
		}
	}
}
