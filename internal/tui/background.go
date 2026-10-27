package tui

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// Background sub-agents, from the TUI's side: the model starts one with the
// Agent tool's run_in_background, it shows in the running-work panel while it
// works, and when it finishes its result goes to the model — the same way a
// message the user typed would. Mid-turn that is the steering queue, which the
// loop reads after each tool batch; idle, it starts a turn of its own, so a
// result is acted on rather than left waiting for the user to say something.

// bgDoneMsg reports a finished background task.
type bgDoneMsg struct{ task agent.BackgroundTask }

// onBackgroundDone records a finished task and hands its result to the model.
func (m *Model) onBackgroundDone(t agent.BackgroundTask) tea.Cmd {
	mark, style, status := "✓", toolStyle, "finished"
	if t.Err != nil {
		mark, style, status = "✗", errStyle, "failed"
		if errors.Is(t.Err, context.Canceled) {
			mark, style, status = "⊘", hintStyle, "stopped"
		}
	}
	m.appendLine(style.Render(fmt.Sprintf("  %s background %s (%s) %s after %s", mark, t.ID, t.Label, status,
		elapsed(t.Ended.Sub(t.Started).Truncate(time.Second)))))
	notice := t.Notice()
	if m.state == stateIdle {
		m.setState(stateRunning)
		return m.startTurn(notice)
	}
	// A turn is running or waiting on the user: the loop picks this up at its
	// next safe point, or doneMsg sends it as the next turn.
	m.steer.add(fmt.Sprintf("[%s %s]", t.ID, status), notice)
	return nil
}

// runningAgents is the background agents still working, for the panel.
func (m *Model) runningAgents() []agent.BackgroundTask {
	if m.bg == nil {
		return nil
	}
	var out []agent.BackgroundTask
	for _, t := range m.bg.List() {
		if t.Running {
			out = append(out, t)
		}
	}
	return out
}
