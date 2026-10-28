package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// turnModel is a model whose RunFunc blocks until its turn is cancelled, so a
// test can hold a turn in flight and drive Update around it. runs counts the
// agent goroutines started. (From PR #67.)
func turnModel(t *testing.T) (*Model, *atomic.Int32) {
	t.Helper()
	m := newTestModel()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // releases any turn goroutine still parked in run
	m.ctx = ctx
	m.events = make(chan tea.Msg, 64)
	runs := &atomic.Int32{}
	m.run = func(ctx context.Context, _ agent.Turn) (agent.Result, error) {
		runs.Add(1)
		<-ctx.Done()
		return agent.Result{}, ctx.Err()
	}
	return m, runs
}

func deliver(m *Model, msg tea.Msg) *Model {
	model, _ := m.Update(msg)
	return model.(*Model)
}

func pressKey(m *Model, code rune) *Model {
	return deliver(m, tea.KeyPressMsg{Code: code})
}

func pressDigit(m *Model, d rune) *Model {
	return deliver(m, tea.KeyPressMsg{Code: d, Text: string(d)})
}

func typeAndEnter(m *Model, text string) *Model {
	m.input.SetValue(text)
	return pressKey(m, tea.KeyEnter)
}

// waitRuns gives turn goroutines a moment to reach run, so the count is a
// fact about what was started rather than about scheduling.
func waitRuns(runs *atomic.Int32, want int32) {
	for i := 0; i < 100 && runs.Load() < want; i++ {
		time.Sleep(5 * time.Millisecond)
	}
}

func startRunningTurn(t *testing.T) (*Model, *atomic.Int32) {
	t.Helper()
	m, runs := turnModel(t)
	m = typeAndEnter(m, "refactor the parser")
	if m.state != stateRunning {
		t.Fatalf("submit did not start a turn: state=%v", m.state)
	}
	waitRuns(runs, 1)
	return m, runs
}

// The reported bug: /mode during a turn opened the picker, and choosing an
// item set the UI idle while the turn ran on, so the next Enter started a
// second agent goroutine alongside the first.
func TestPickerChosenMidTurnReturnsToRunning(t *testing.T) {
	m, runs := startRunningTurn(t)
	before := m.currentMode()

	m = typeAndEnter(m, "/mode")
	if m.state != stateAwaitingChoice {
		t.Fatalf("/mode mid-turn: state = %v, want the picker", m.state)
	}
	m = pressDigit(m, '2')
	if m.state != stateRunning {
		t.Fatalf("choosing from a picker opened mid-turn left state = %v, want running", m.state)
	}
	if m.currentMode() == before {
		t.Errorf("the choice was not applied: mode still %q", before)
	}
	if m.turnCancel == nil {
		t.Error("the turn lost its cancel func")
	}

	// Enter now queues a follow-up, as in any running turn.
	m = typeAndEnter(m, "and add tests")
	if got := m.steer.drain(); got.Text != "and add tests" {
		t.Errorf("follow-up not queued: %+v", got)
	}
	waitRuns(runs, 2)
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d agent goroutines started, want 1", n)
	}
}

// Esc on a picker opened mid-turn closes the picker; it is not "interrupt".
func TestEscInPickerMidTurnKeepsTheTurn(t *testing.T) {
	m, _ := startRunningTurn(t)
	cancelled := false
	orig := m.turnCancel
	m.turnCancel = func() { cancelled = true; orig() }

	m = typeAndEnter(m, "/mode")
	m = pressKey(m, tea.KeyEscape)
	if cancelled {
		t.Fatal("Esc in the picker cancelled the running turn")
	}
	if m.state != stateRunning || m.choiceItems != nil {
		t.Fatalf("Esc in a mid-turn picker: state = %v, items %d; want running, none", m.state, len(m.choiceItems))
	}
	// With the picker gone, Esc means "interrupt" again.
	pressKey(m, tea.KeyEscape)
	if !cancelled {
		t.Error("Esc with no picker open should interrupt the turn")
	}
}

func TestEscInPickerWhileIdleReturnsIdle(t *testing.T) {
	m, _ := turnModel(t)
	m = typeAndEnter(m, "/mode")
	m = pressKey(m, tea.KeyEscape)
	if m.state != stateIdle {
		t.Fatalf("state = %v, want idle", m.state)
	}
}

// /model fetches off the UI goroutine, so its list can land mid-turn.
func TestModelListArrivingMidTurnReturnsToRunning(t *testing.T) {
	m, runs := startRunningTurn(t)
	m = deliver(m, modelsMsg{models: []api.ModelInfo{{ID: "claude-opus-5"}, {ID: "claude-sonnet-5"}}})
	if m.state != stateAwaitingChoice {
		t.Fatalf("state = %v, want the model picker", m.state)
	}
	m = pressDigit(m, '2')
	if m.state != stateRunning || m.sess.Model != "claude-sonnet-5" {
		t.Fatalf("state = %v, model %q; want running on claude-sonnet-5", m.state, m.sess.Model)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d agent goroutines started, want 1", n)
	}
}

// A model list landing while the turn waits on an approval must not hide it.
func TestModelListDoesNotHideAPendingApproval(t *testing.T) {
	m, _ := startRunningTurn(t)
	m = deliver(m, permissionMsg{req: agent.ApprovalRequest{ToolName: "Bash"}, reply: make(chan permission.Decision, 1)})
	if m.state != stateAwaitingPermission {
		t.Fatalf("state = %v, want the approval prompt", m.state)
	}
	m = deliver(m, modelsMsg{models: []api.ModelInfo{{ID: "claude-opus-5"}}})
	if m.state != stateAwaitingPermission {
		t.Fatalf("a model list replaced the approval prompt: state = %v", m.state)
	}
}

// A turn ending under an open picker leaves it up; closing it then returns
// to idle, not to a running state with no turn behind it.
func TestTurnEndingUnderPickerReturnsIdle(t *testing.T) {
	m, _ := startRunningTurn(t)
	m = typeAndEnter(m, "/mode")
	m = deliver(m, doneMsg{res: agent.Result{StopReason: "end_turn", Text: "done"}})
	if m.state != stateAwaitingChoice {
		t.Fatalf("the turn ending closed the picker: state = %v", m.state)
	}
	m = pressDigit(m, '2')
	if m.state != stateIdle {
		t.Fatalf("state = %v after the turn ended, want idle", m.state)
	}
}

// The running turn's prompt takes over from an open picker.
func TestApprovalClosesAMidTurnPicker(t *testing.T) {
	m, _ := startRunningTurn(t)
	m = typeAndEnter(m, "/mode")
	m = deliver(m, permissionMsg{req: agent.ApprovalRequest{ToolName: "Bash"}, reply: make(chan permission.Decision, 1)})
	if m.state != stateAwaitingPermission || m.choiceItems != nil {
		t.Fatalf("state = %v, items %d; want the approval prompt and no picker", m.state, len(m.choiceItems))
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "picker closed") {
		t.Error("the user was not told the picker closed")
	}
}
