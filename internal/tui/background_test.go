package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

func finishedTask() agent.BackgroundTask {
	start := time.Now().Add(-time.Minute)
	return agent.BackgroundTask{ID: "agent_1", Label: "Explore: survey", Started: start, Ended: start.Add(time.Minute), Result: "three findings"}
}

// Mid-turn, a finished background agent's result joins the steering queue the
// loop reads after each tool batch.
func TestBackgroundResultMidTurnIsSteered(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	m.state = stateRunning
	m.onBackgroundDone(finishedTask())
	if got := peekSteerPrompt(m); !strings.Contains(got, "three findings") || !strings.Contains(got, `id="agent_1"`) {
		t.Errorf("queued for the model: %q", got)
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "✓ background agent_1 (Explore: survey) finished after 1m00s") {
		t.Errorf("scrollback:\n%s", out)
	}
}

// Idle, it starts a turn of its own, so the result is acted on.
func TestBackgroundResultWhenIdleStartsATurn(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	m.ctx = t.Context()
	prompts := make(chan string, 1)
	m.run = func(_ context.Context, turn agent.Turn) (agent.Result, error) {
		prompts <- turn.Prompt
		return agent.Result{}, nil
	}
	if cmd := m.onBackgroundDone(finishedTask()); cmd == nil {
		t.Fatal("idle: want a turn started")
	}
	select {
	case p := <-prompts:
		if !strings.Contains(p, "three findings") {
			t.Errorf("turn prompt = %q", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no turn ran")
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want running", m.state)
	}
}

// Running agents show in the panel and the status line.
func TestRunningAgentsInPanel(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	m.bg = agent.NewBackground(t.Context(), nil)
	release := make(chan struct{})
	defer close(release)
	m.bg.Start("Explore: survey the MCP code", func(ctx context.Context, progress func(string)) (string, error) {
		progress("Read mcp.go")
		<-release
		return "", nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		view := stripANSI(m.bottomView())
		if strings.Contains(view, "⏵ agent_1") && strings.Contains(view, "Read mcp.go") && strings.Contains(view, "1 agent") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent not shown:\n%s", view)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if m.jobsTick() == nil {
		t.Error("the panel should tick while an agent runs")
	}
}
