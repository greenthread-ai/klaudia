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

// systemCapture records the system blocks of every request, which is the only
// way to tell whether the model was actually told anything.
type systemCapture struct {
	mu    sync.Mutex
	turns [][]anthropic.BetaTextBlockParam
	reply []anthropic.BetaMessage
	n     int
}

func (c *systemCapture) StreamTurn(_ context.Context, p anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	c.mu.Lock()
	c.turns = append(c.turns, p.System)
	i := c.n
	c.n++
	c.mu.Unlock()
	if i < len(c.reply) {
		return c.reply[i], nil
	}
	return anthropic.BetaMessage{StopReason: "end_turn"}, nil
}

func (c *systemCapture) systemAt(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, blk := range c.turns[i] {
		b.WriteString(blk.Text)
	}
	return b.String()
}

// The defect: nothing ever told the model which permission mode it was working
// under. In plan mode it reasoned its way to an edit, called Edit, and was
// refused — a wasted turn, and a plan built without knowing that running the
// build to check it had never been an option.
func TestPlanModeIsInTheSystemPrompt(t *testing.T) {
	got := blocksText(systemFor("BASE PROMPT", permission.ModePlan))
	if !strings.Contains(got, "BASE PROMPT") {
		t.Error("the base prompt was lost")
	}
	if !strings.Contains(got, "plan mode") {
		t.Errorf("the model was not told it is in plan mode: %q", got)
	}
	if !strings.Contains(got, "ExitPlanMode") {
		t.Errorf("the clause should say how to get out: %q", got)
	}
}

func TestBypassModeIsInTheSystemPrompt(t *testing.T) {
	got := blocksText(systemFor("BASE PROMPT", permission.ModeBypassPermissions))
	if !strings.Contains(got, "host gate") {
		t.Errorf("the model was not told the gate is off: %q", got)
	}
}

// Autonomous is what the base prompt already assumes, so restating it on every
// turn is tokens spent to change nothing.
func TestAutonomousAddsNoClause(t *testing.T) {
	blocks := systemFor("BASE PROMPT", permission.ModeAutonomous)
	if len(blocks) != 1 {
		t.Fatalf("got %d system blocks, want 1: %v", len(blocks), blocks)
	}
	if blocks[0].Text != "BASE PROMPT" {
		t.Errorf("system = %q, want the base prompt unchanged", blocks[0].Text)
	}
}

// The clause is a separate block so the long, identical prefix stays
// byte-for-byte stable across a mode change and keeps whatever prompt caching
// it has earned.
func TestTheModeClauseIsASeparateBlock(t *testing.T) {
	for _, mode := range []permission.Mode{permission.ModePlan, permission.ModeBypassPermissions} {
		blocks := systemFor("BASE PROMPT", mode)
		if len(blocks) != 2 {
			t.Fatalf("%s: got %d blocks, want 2", mode, len(blocks))
		}
		if blocks[0].Text != "BASE PROMPT" {
			t.Errorf("%s: the base block was modified: %q", mode, blocks[0].Text)
		}
	}
}

// An empty base prompt must not produce an empty leading block, which the API
// rejects.
func TestNoEmptyBaseBlock(t *testing.T) {
	blocks := systemFor("", permission.ModePlan)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	if blocks[0].Text == "" {
		t.Error("an empty system block was emitted")
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
	provider := &systemCapture{reply: []anthropic.BetaMessage{asst}}

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

	if got := provider.systemAt(0); !strings.Contains(got, "plan mode") {
		t.Errorf("first request did not tell the model it was in plan mode")
	}
	if got := provider.systemAt(1); strings.Contains(got, "plan mode") {
		t.Errorf("the second request still claimed plan mode after the switch:\n%s", got)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"plan", "autonomous"}
	if len(modes) != len(want) || modes[0] != want[0] || modes[1] != want[1] {
		t.Errorf("permission_mode events = %v, want %v", modes, want)
	}
}

func blocksText(blocks []anthropic.BetaTextBlockParam) string {
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
	}
	return b.String()
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
