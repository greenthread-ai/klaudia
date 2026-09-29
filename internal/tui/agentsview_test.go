package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/agent"
)

type fakeBackgroundAgents struct{ agents []agent.BackgroundAgent }

func (f *fakeBackgroundAgents) List() []agent.BackgroundAgent { return f.agents }

// The /agents view lists each background agent with its id, type, status and
// elapsed time — the status view the issue asks for.
func TestRenderBackgroundAgentsListsStates(t *testing.T) {
	now := time.Now()
	out := renderBackgroundAgents([]agent.BackgroundAgent{
		{
			ID: "agent-1", Type: "Explore", Label: "search callers",
			Status: agent.BackgroundSucceeded, StartedAt: now.Add(-90 * time.Second),
			FinishedAt: now.Add(-30 * time.Second), Result: "done",
		},
		{
			ID: "agent-2", Type: "general-purpose", Label: "refactor api",
			Status: agent.BackgroundRunning, StartedAt: now.Add(-10 * time.Second),
			Activity: "Edit internal/api/client.go", Isolated: true,
		},
	})
	for _, want := range []string{"agent-1", "Explore", "succeeded", "search callers",
		"agent-2", "general-purpose", "running", "1m00s"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q:\n%s", want, out)
		}
	}
	// A running agent shows its current activity.
	if !strings.Contains(out, "Edit internal/api/client.go") {
		t.Errorf("running agent's activity not shown:\n%s", out)
	}
}

// Running agents are listed before finished ones.
func TestRenderBackgroundAgentsRunningFirst(t *testing.T) {
	out := renderBackgroundAgents([]agent.BackgroundAgent{
		{ID: "agent-1", Type: "Explore", Status: agent.BackgroundSucceeded},
		{ID: "agent-2", Type: "Explore", Status: agent.BackgroundRunning},
	})
	if strings.Index(out, "agent-2") > strings.Index(out, "agent-1") {
		t.Errorf("running agent should sort before the finished one:\n%s", out)
	}
}

// No background agents means the view says nothing (only the type list shows).
func TestRenderBackgroundAgentsEmpty(t *testing.T) {
	if got := renderBackgroundAgents(nil); got != "" {
		t.Errorf("empty listing should render nothing, got %q", got)
	}
}

// A failed agent's error is surfaced in the view.
func TestRenderBackgroundAgentsShowsError(t *testing.T) {
	out := renderBackgroundAgents([]agent.BackgroundAgent{
		{ID: "agent-1", Type: "general-purpose", Status: agent.BackgroundFailed, Err: "could not isolate a worktree"},
	})
	if !strings.Contains(out, "could not isolate a worktree") {
		t.Errorf("failed agent's error not shown:\n%s", out)
	}
}

// The /agents handler appends the background section to the type list when the
// session has a lister.
func TestAgentsCommandIncludesBackgroundSection(t *testing.T) {
	m := &Model{sess: &Session{
		Agents: []AgentInfo{{Name: "Explore", Description: "read only"}},
		BackgroundAgents: &fakeBackgroundAgents{agents: []agent.BackgroundAgent{
			{ID: "agent-1", Type: "Explore", Status: agent.BackgroundRunning, StartedAt: time.Now()},
		}},
	}, height: 40}
	got := m.renderAgents()
	if sec := m.backgroundAgentsSection(); sec != "" {
		got += "\n\n" + sec
	}
	if !strings.Contains(got, "Explore") || !strings.Contains(got, "agent-1") {
		t.Errorf("combined /agents output missing a half:\n%s", got)
	}
}
