package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// msgCapture records the system blocks and messages of every request, which
// is the only way to tell whether the model was actually told anything.
type msgCapture struct {
	mu    sync.Mutex
	turns [][]anthropic.BetaTextBlockParam
	msgs  [][]anthropic.BetaMessageParam
	reply []anthropic.BetaMessage
	n     int
}

func (c *msgCapture) StreamTurn(_ context.Context, p anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	c.mu.Lock()
	c.turns = append(c.turns, p.System)
	c.msgs = append(c.msgs, p.Messages)
	i := c.n
	c.n++
	c.mu.Unlock()
	if i < len(c.reply) {
		return c.reply[i], nil
	}
	return anthropic.BetaMessage{StopReason: "end_turn"}, nil
}

func (c *msgCapture) systemAt(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, blk := range c.turns[i] {
		b.WriteString(blk.Text)
	}
	return b.String()
}

// lastUserText is the text of the newest user message of request i, which is
// where the loop tells the model its mode.
func (c *msgCapture) lastUserText(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	msgs := c.msgs[i]
	var b strings.Builder
	for _, blk := range msgs[len(msgs)-1].Content {
		if blk.OfText != nil {
			b.WriteString(blk.OfText.Text)
		}
	}
	return b.String()
}

// The defect: nothing ever told the model which permission mode it was working
// under. In plan mode it reasoned its way to an edit, called Edit, and was
// refused — a wasted turn, and a plan built without knowing that running the
// build to check it had never been an option.
func TestPlanModeIsToldInTheConversation(t *testing.T) {
	msg := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("go"))
	tellMode(nil, &msg, permission.ModePlan)
	got := msg.Content[len(msg.Content)-1].OfText.Text
	if len(msg.Content) != 2 || !strings.Contains(got, "plan mode") || !strings.Contains(got, "ExitPlanMode") {
		t.Errorf("plan mode clause = %q; want it added after the prompt, with the way out", got)
	}
	msg = anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("go"))
	tellMode(nil, &msg, permission.ModeBypassPermissions)
	if !strings.Contains(msg.Content[1].OfText.Text, "host gate") {
		t.Errorf("the model was not told the gate is off: %q", msg.Content[1].OfText.Text)
	}
}

// Autonomous is what the base prompt already assumes: nothing is said until
// the conversation has been told something else, and then the change back is
// said once.
func TestModeIsToldOnlyWhenItChanges(t *testing.T) {
	told := func(history []anthropic.BetaMessageParam, mode permission.Mode) anthropic.BetaMessageParam {
		msg := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("next"))
		tellMode(history, &msg, mode)
		return msg
	}
	if m := told(nil, permission.ModeAutonomous); len(m.Content) != 1 {
		t.Error("autonomous was announced to a conversation that was told nothing else")
	}
	history := []anthropic.BetaMessageParam{told(nil, permission.ModePlan)}
	if m := told(history, permission.ModePlan); len(m.Content) != 1 {
		t.Error("plan mode was told twice")
	}
	back := told(history, permission.ModeAutonomous)
	if len(back.Content) != 2 || !strings.Contains(back.Content[1].OfText.Text, "now autonomous") {
		t.Error("leaving plan mode was not said")
	}
	if m := told(append(history, back), permission.ModeAutonomous); len(m.Content) != 1 {
		t.Error("the change back was told twice")
	}
}

// The system prompt is the base prompt in every mode: a changed system prompt
// invalidates preserved thinking (PR #63). An empty base yields no block,
// which the API would reject.
func TestSystemPromptDoesNotCarryTheMode(t *testing.T) {
	if got := systemFor("BASE PROMPT"); len(got) != 1 || got[0].Text != "BASE PROMPT" {
		t.Errorf("system = %v", got)
	}
	if got := systemFor(""); len(got) != 0 {
		t.Errorf("empty base produced %d blocks", len(got))
	}
}

func TestModeTrackerAnnouncesChangesOnce(t *testing.T) {
	var tr modeTracker
	tests := []struct {
		mode permission.Mode
		want bool
	}{
		// The first turn reports whatever it starts in: a resumed session can
		// start in plan mode, and "nothing was said, so it must be
		// autonomous" is the assumption that made this invisible.
		{permission.ModeAutonomous, true},
		{permission.ModeAutonomous, false},
		{permission.ModePlan, true},
		{permission.ModePlan, false},
		{permission.ModeAutonomous, true},
	}
	for i, tt := range tests {
		if got := tr.changed(tt.mode); got != tt.want {
			t.Errorf("step %d: changed(%s) = %v, want %v", i, tt.mode, got, tt.want)
		}
	}
}

// The end-to-end property: a mode change between turns reaches both the model
// and the frontend. Approving a plan flips the mode mid-run, so this cannot be
// done once at the top of Run.
func TestAMidRunModeChangeReachesTheModelAndTheFrontend(t *testing.T) {
	mode := permission.ModePlan
	var mu sync.Mutex
	current := func() permission.Mode {
		mu.Lock()
		defer mu.Unlock()
		return mode
	}

	// Two turns: the first calls a tool, which flips the mode as a side effect
	// the way approving a plan does; the second is the request that must carry
	// the new clause.
	flip := &flipTool{onRun: func() {
		mu.Lock()
		mode = permission.ModeAutonomous
		mu.Unlock()
	}}
	// Built via JSON so the SDK populates the union's raw fields; a struct
	// literal leaves dispatch unable to read the tool name.
	var asst anthropic.BetaMessage
	if err := json.Unmarshal([]byte(`{
		"role": "assistant",
		"stop_reason": "tool_use",
		"content": [{"type": "tool_use", "id": "t1", "name": "Flip", "input": {}}]
	}`), &asst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	provider := &msgCapture{reply: []anthropic.BetaMessage{asst}}

	l := New(provider, tools.NewRegistry(flip))
	var modes []string
	emit := func(ev Event) {
		if ev.Type == "permission_mode" {
			mu.Lock()
			modes = append(modes, ev.Content)
			mu.Unlock()
		}
	}
	if _, err := l.Run(context.Background(), Options{
		Prompt:     "do it",
		System:     "BASE PROMPT",
		Model:      "test",
		Permission: permission.Context{Mode: current},
	}, emit); err != nil {
		t.Fatalf("run: %v", err)
	}

	if got := provider.lastUserText(0); !strings.Contains(got, "plan mode") {
		t.Errorf("first request did not tell the model it was in plan mode: %q", got)
	}
	if got := provider.lastUserText(1); !strings.Contains(got, "now autonomous") {
		t.Errorf("the second request did not say plan mode had ended: %q", got)
	}
	if provider.systemAt(0) != provider.systemAt(1) {
		t.Error("the system prompt changed with the mode")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"plan", "autonomous"}
	if len(modes) != len(want) || modes[0] != want[0] || modes[1] != want[1] {
		t.Errorf("permission_mode events = %v, want %v", modes, want)
	}
}

// flipTool runs a side effect, standing in for the user approving a plan while
// the turn is in flight.
type flipTool struct{ onRun func() }

func (flipTool) Name() string                                { return "Flip" }
func (flipTool) Description(context.Context) (string, error) { return "", nil }
func (flipTool) InputSchema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (flipTool) ValidateInput(json.RawMessage) error         { return nil }
func (flipTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (flipTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (f flipTool) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	f.onRun()
	return []tools.Result{{Content: "flipped"}}, nil
}
