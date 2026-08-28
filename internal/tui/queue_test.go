package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestQueueWhileRunning(t *testing.T) {
	m := newTestModel()
	m.state = stateRunning
	cancelled := false
	m.turnCancel = func() { cancelled = true }

	// Type a follow-up and press Enter → it's queued, input cleared.
	m.input.SetValue("also add tests")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if peekSteer(m) != "also add tests" {
		t.Fatalf("queued = %q, want the typed text", peekSteer(m))
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		t.Errorf("input should be cleared after queueing, got %q", m.input.Value())
	}

	// Enter again on an empty input → interrupt the running turn.
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !cancelled {
		t.Error("a second Enter (empty) should interrupt the turn to send the queued message")
	}
	// The message is captured at interrupt time, out of the box, so a late agent
	// poll can't drain it into the dying turn. The box is now empty; doneMsg
	// sends the captured copy.
	if peekSteer(m) != "" {
		t.Errorf("the interrupt should have drained the box, still %q", peekSteer(m))
	}
	if m.interruptResend.Text != "also add tests" {
		t.Errorf("interrupt did not capture the message, got %q", m.interruptResend.Text)
	}
	m.interruptResend = agent.Interjection{} // consumed by doneMsg in a real run

	// ↑ recalls the queued message into the input for editing (and clears the queue).
	m.input.SetValue("v2")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter}) // re-queue "v2"
	m.onKey(tea.KeyMsg{Type: tea.KeyUp})    // recall to edit
	if peekSteer(m) != "" {
		t.Errorf("queue should clear when recalled for editing, still %q", peekSteer(m))
	}
	if m.input.Value() != "v2" {
		t.Errorf("recalled input = %q, want v2", m.input.Value())
	}
}

func TestQueueEnterWithoutTextOrQueueIsNoop(t *testing.T) {
	m := newTestModel()
	m.state = stateRunning
	// Empty input, nothing queued → Enter does nothing (no panic, no queue).
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if peekSteer(m) != "" {
		t.Errorf("nothing should be queued, got %q", peekSteer(m))
	}
}

// The reported bug: interrupt-to-send during a tool call left the message
// stranded because the agent's post-batch poll drained the box after the
// cancel. The fix captures the message at interrupt time, so even if the box is
// empty by doneMsg, the message is still sent.
func TestInterruptCapturesMessageBeforeAgentCanDrainIt(t *testing.T) {
	m := newTestModel()
	m.ctx = context.Background()
	m.state = stateRunning
	m.turnCancel = func() {}

	m.input.SetValue("actually, do this instead")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter}) // queue
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter}) // interrupt-to-send

	// Simulate the race: the agent's poll fires after the cancel and drains the
	// box. With the fix the box is already empty, so it gets nothing.
	if got := m.steer.drain(); !got.Empty() {
		t.Fatalf("the agent poll drained %q — it should have been captured already", got.Text)
	}

	// doneMsg with a cancelled turn must still send the captured message.
	m.run = func(ctx context.Context, prompt string, _ []anthropic.BetaMessageParam,
		_ agent.Approver, _ tools.Asker, _ tools.Planner, _ agent.Emitter,
		_ func() agent.Interjection, _ func(string, []string)) (agent.Result, error) {
		return agent.Result{}, nil
	}
	m.update(doneMsg{res: agent.Result{}, err: context.Canceled})

	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "actually, do this instead") {
		t.Errorf("the interrupted message was not sent as the next turn:\n%s", out)
	}
	if m.state != stateRunning {
		t.Errorf("no follow-up turn started; state = %v", m.state)
	}
}

// A message queued during a turn that ends NORMALLY (agent never polled) still
// resends from the box — the interrupt path must not have broken that.
func TestNaturalEndStillResendsAQueuedMessage(t *testing.T) {
	m := newTestModel()
	m.ctx = context.Background()
	m.state = stateRunning
	m.run = func(ctx context.Context, prompt string, _ []anthropic.BetaMessageParam,
		_ agent.Approver, _ tools.Asker, _ tools.Planner, _ agent.Emitter,
		_ func() agent.Interjection, _ func(string, []string)) (agent.Result, error) {
		return agent.Result{}, nil
	}
	m.steer.add("follow-up question")
	m.update(doneMsg{res: agent.Result{StopReason: "end_turn", Text: "answer"}})
	if !strings.Contains(stripANSI(m.transcript.String()), "follow-up question") {
		t.Error("a message queued before a natural turn-end was not resent")
	}
}

// After an interrupt, the resend turn carries a note about jobs left running so
// the model can decide their fate. Killing them is the model's call, not
// automatic.
func TestInterruptResendCarriesRunningJobsNote(t *testing.T) {
	m := newTestModel()
	m.sess.Jobs = &fakeJobs{jobs: []tools.JobStatus{
		{ID: "bash_1", Name: "dev", Running: true, Port: "3000"},
		{ID: "bash_2", Name: "old", Running: false},
	}}
	note := m.runningJobsNote()
	if !strings.Contains(note, "dev") || !strings.Contains(note, "3000") {
		t.Errorf("note does not name the running job: %q", note)
	}
	if strings.Contains(note, "old") {
		t.Errorf("note lists an exited job: %q", note)
	}
	if strings.Contains(strings.ToLower(note), "kill them") {
		t.Errorf("note orders a kill rather than leaving it to the model: %q", note)
	}
	// No jobs → no note.
	m.sess.Jobs = &fakeJobs{}
	if n := m.runningJobsNote(); n != "" {
		t.Errorf("a note appeared with no running jobs: %q", n)
	}
}

// Queuing a message must not write anything to scrollback — the queued state is
// transient and lives only in the composited live region. Writing it via
// appendLine baked a permanent, duplicate hint into the transcript every time,
// which is the "smearing" a real session showed.
func TestQueuingWritesNothingToScrollback(t *testing.T) {
	m := newTestModel()
	m.state = stateRunning
	m.turnCancel = func() {}

	m.input.SetValue("a follow-up")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})

	if got := stripANSI(m.transcript.String()); strings.TrimSpace(got) != "" {
		t.Errorf("queuing left a line in scrollback: %q", got)
	}
	// The live region is where the queued state shows, and it does.
	if !strings.Contains(stripANSI(m.renderQueuedHint()), "a follow-up") {
		t.Error("the queued message is not shown in the live region")
	}
}
