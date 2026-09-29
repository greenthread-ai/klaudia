package tui

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// turnModel is a model whose RunFunc blocks until its turn is cancelled, so a
// test can hold a turn in flight and drive Update around it. runs counts how
// many agent goroutines were started.
func turnModel(t *testing.T) (*Model, *atomic.Int32) {
	t.Helper()
	m := newTestModel()
	m.resize(100, 40)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // releases any turn goroutine still parked in run
	m.ctx = ctx
	m.events = make(chan tea.Msg, 64)
	runs := &atomic.Int32{}
	m.run = func(ctx context.Context, _ string, _ []anthropic.BetaMessageParam,
		_ agent.Approver, _ tools.Asker, _ tools.Planner, _ agent.Emitter,
		_ func() agent.Interjection, _ func(string, []string)) (agent.Result, error) {
		runs.Add(1)
		<-ctx.Done()
		return agent.Result{}, ctx.Err()
	}
	return m, runs
}

func press(m *Model, msg tea.KeyMsg) *Model {
	model, _ := m.Update(msg)
	return model.(*Model)
}

func typeAndEnter(m *Model, text string) *Model {
	m.input.SetValue(text)
	return press(m, tea.KeyMsg{Type: tea.KeyEnter})
}

func digit(d string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(d)} }

// waitRuns gives the turn goroutine(s) a moment to reach run, so the count is
// a fact about what was started rather than about scheduling.
func waitRuns(t *testing.T, runs *atomic.Int32, want int32) {
	t.Helper()
	for i := 0; i < 100 && runs.Load() < want; i++ {
		time.Sleep(5 * time.Millisecond)
	}
}

// startRunningTurn submits a prompt from idle and checks the turn is in flight.
func startRunningTurn(t *testing.T) (*Model, *atomic.Int32) {
	t.Helper()
	m, runs := turnModel(t)
	m = typeAndEnter(m, "refactor the parser")
	if m.state != stateRunning || !m.turnInFlight {
		t.Fatalf("submit did not start a turn: state=%v inFlight=%v", m.state, m.turnInFlight)
	}
	waitRuns(t, runs, 1)
	return m, runs
}

// The reported bug: /mode during a turn opened the picker, and choosing an item
// set the UI idle while the turn ran on. The idle prompt then took Enter as a
// new prompt and started a second agent goroutine alongside the first.
func TestPickerChosenMidTurnReturnsToRunning(t *testing.T) {
	m, runs := startRunningTurn(t)

	m = typeAndEnter(m, "/mode")
	if m.state != stateAwaitingChoice {
		t.Fatalf("/mode mid-turn: state = %v, want the picker", m.state)
	}

	m = press(m, digit("2")) // plan mode
	if m.state != stateRunning {
		t.Fatalf("choosing from a picker opened mid-turn left state = %v, want running", m.state)
	}
	if m.sess.PermissionMode != string(permission.ModePlan) {
		t.Errorf("the choice was not applied: mode = %q", m.sess.PermissionMode)
	}
	if m.turnCancel == nil {
		t.Error("the turn lost its cancel func")
	}

	// Enter now queues a follow-up, as it does in any running turn; it must
	// not start a second turn.
	m = typeAndEnter(m, "and add tests")
	if got := peekSteer(m); got != "and add tests" {
		t.Errorf("follow-up not queued, box = %q", got)
	}
	waitRuns(t, runs, 2)
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d agent goroutines started, want 1", n)
	}
}

// Esc on a picker opened mid-turn closes the picker; it is not "interrupt".
// The on-screen hint says "esc to cancel", and the Esc interrupt check used to
// run before the picker saw the key.
func TestEscInPickerMidTurnKeepsTheTurn(t *testing.T) {
	m, _ := startRunningTurn(t)
	cancelled := false
	orig := m.turnCancel
	m.turnCancel = func() { cancelled = true; orig() }

	m = typeAndEnter(m, "/mode")
	if m.state != stateAwaitingChoice {
		t.Fatalf("state = %v, want the picker", m.state)
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if cancelled {
		t.Fatal("Esc in the picker cancelled the running turn")
	}
	if m.state != stateRunning {
		t.Fatalf("Esc in a mid-turn picker left state = %v, want running", m.state)
	}
	if m.choiceItems != nil {
		t.Error("picker items not cleared")
	}

	// With the picker gone, Esc goes back to meaning "interrupt".
	press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Esc with no picker open should interrupt the turn")
	}
}

// Esc in a picker opened while idle still cancels back to idle.
func TestEscInPickerWhileIdleReturnsIdle(t *testing.T) {
	m, _ := turnModel(t)
	m = typeAndEnter(m, "/mode")
	m = press(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateIdle {
		t.Fatalf("state = %v, want idle", m.state)
	}
}

// /model fetches off the UI goroutine, so its list can land after a turn has
// started (or be asked for mid-turn). The picker it opens must hand back to the
// running turn too.
func TestModelListArrivingMidTurnReturnsToRunning(t *testing.T) {
	m, runs := startRunningTurn(t)

	m = deliver(m, modelsMsg{models: []api.ModelInfo{
		{ID: "claude-opus-5", DisplayName: "Claude Opus 5"},
		{ID: "claude-sonnet-5", DisplayName: "Claude Sonnet 5"},
	}})
	if m.state != stateAwaitingChoice {
		t.Fatalf("state = %v, want the model picker", m.state)
	}
	m = press(m, digit("2"))
	if m.state != stateRunning {
		t.Fatalf("state = %v after choosing a model mid-turn, want running", m.state)
	}
	if m.sess.Model != "claude-sonnet-5" {
		t.Errorf("model = %q, want claude-sonnet-5", m.sess.Model)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d agent goroutines started, want 1", n)
	}
}

// A model list that lands while the turn is blocked on an approval must not
// replace the approval prompt: the agent goroutine is waiting on that answer.
func TestModelListDoesNotHideAPendingApproval(t *testing.T) {
	m, _ := startRunningTurn(t)
	reply := make(chan permission.Decision, 1)
	m = deliver(m, permissionMsg{req: agent.ApprovalRequest{ToolName: "Bash"}, reply: reply})
	if m.state != stateAwaitingPermission {
		t.Fatalf("state = %v, want the approval prompt", m.state)
	}
	m = deliver(m, modelsMsg{models: []api.ModelInfo{{ID: "claude-opus-5"}}})
	if m.state != stateAwaitingPermission {
		t.Fatalf("a model list replaced the approval prompt: state = %v", m.state)
	}
}

// A turn that ends while a picker is open leaves the picker up, and closing it
// then returns to idle — not to a running state with no turn behind it.
func TestTurnEndingUnderPickerReturnsIdle(t *testing.T) {
	m, _ := startRunningTurn(t)
	m = typeAndEnter(m, "/mode")
	m = deliver(m, doneMsg{res: agent.Result{StopReason: "end_turn", Text: "done"}})
	if m.state != stateAwaitingChoice {
		t.Fatalf("the turn ending closed the picker: state = %v", m.state)
	}
	if m.turnInFlight {
		t.Error("turnInFlight still set after doneMsg")
	}
	m = press(m, digit("2"))
	if m.state != stateIdle {
		t.Fatalf("state = %v after the turn ended, want idle", m.state)
	}
}

// When the running turn asks for something, its prompt takes over from the
// picker rather than the picker's items lingering behind it.
func TestApprovalClosesAMidTurnPicker(t *testing.T) {
	m, _ := startRunningTurn(t)
	m = typeAndEnter(m, "/mode")
	m = deliver(m, permissionMsg{req: agent.ApprovalRequest{ToolName: "Bash"}, reply: make(chan permission.Decision, 1)})
	if m.state != stateAwaitingPermission {
		t.Fatalf("state = %v, want the approval prompt", m.state)
	}
	if m.choiceItems != nil {
		t.Error("stale picker items left behind the approval prompt")
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "picker closed") {
		t.Error("the user was not told the picker closed")
	}
}

// startTurn is the backstop: whatever path reaches it, a second turn does not
// start while one is in flight — including one already interrupted but not yet
// finished, whose turnCancel is nil.
func TestStartTurnRefusesASecondConcurrentTurn(t *testing.T) {
	m, runs := startRunningTurn(t)

	if cmd := m.startTurn("a second prompt"); cmd != nil {
		t.Fatal("startTurn started a second turn while one was in flight")
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "already running") {
		t.Error("the refusal was not reported")
	}

	m.interruptTurn() // cancelled, goroutine still winding down
	if cmd := m.startTurn("after interrupt"); cmd != nil {
		t.Fatal("startTurn started a turn while the interrupted one was still finishing")
	}
	waitRuns(t, runs, 2)
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d agent goroutines started, want 1", n)
	}

	// Once the first turn reports done, a new one may start.
	m = deliver(m, doneMsg{err: context.Canceled})
	if cmd := m.startTurn("next"); cmd == nil {
		t.Fatal("startTurn refused after the previous turn finished")
	}
	waitRuns(t, runs, 2)
	if n := runs.Load(); n != 2 {
		t.Fatalf("%d agent goroutines started, want 2", n)
	}
}

// /logs --errors hands the failure lines to the model as a turn. It used to
// start that turn without entering the running state: no spinner, Esc had
// nothing to say it could interrupt, and Enter at the still-idle prompt started
// a second turn.
func TestLogsErrorsStartsARunningTurn(t *testing.T) {
	m, runs := turnModel(t)
	m.sess.Jobs = &fakeJobs{
		jobs: []tools.JobStatus{{ID: "bash_1", Name: "api", Running: true}},
		logs: map[string]string{"api": "listening\nerror: connection refused\n"},
	}

	m = typeAndEnter(m, "/logs --errors api")
	if m.state != stateRunning || !m.turnInFlight {
		t.Fatalf("/logs --errors: state=%v inFlight=%v, want a running turn", m.state, m.turnInFlight)
	}
	waitRuns(t, runs, 1)

	m = typeAndEnter(m, "also check the port")
	if got := peekSteer(m); got != "also check the port" {
		t.Errorf("Enter did not queue a follow-up, box = %q", got)
	}
	waitRuns(t, runs, 2)
	if n := runs.Load(); n != 1 {
		t.Fatalf("%d agent goroutines started, want 1", n)
	}
}

// deliver hands a non-key message to Update.
func deliver(m *Model, msg tea.Msg) *Model {
	model, _ := m.Update(msg)
	return model.(*Model)
}
