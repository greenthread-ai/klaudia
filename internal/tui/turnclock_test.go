package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

func TestTurnClock(t *testing.T) {
	t0 := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	tests := []struct {
		name                 string
		steps                func(c *turnClock)
		now                  time.Duration
		total, wait, working time.Duration
	}{
		{name: "an uninterrupted turn", steps: func(c *turnClock) { c.begin(t0) },
			now: 90 * time.Second, total: 90 * time.Second, working: 90 * time.Second},
		{
			// The reported case: a question asked a minute in, answered
			// 5h41m later, then 40s more work.
			name: "a question left overnight is waiting, not working",
			steps: func(c *turnClock) {
				c.begin(t0)
				c.pause(at(time.Minute))
				c.resume(at(time.Minute + 5*time.Hour + 41*time.Minute))
			},
			now:   time.Minute + 5*time.Hour + 41*time.Minute + 40*time.Second,
			total: time.Minute + 5*time.Hour + 41*time.Minute + 40*time.Second,
			wait:  5*time.Hour + 41*time.Minute, working: 100 * time.Second,
		},
		{name: "a wait still in progress counts as waiting",
			steps: func(c *turnClock) { c.begin(t0); c.pause(at(10 * time.Second)) },
			now:   10 * time.Minute, total: 10 * time.Minute, wait: 10*time.Minute - 10*time.Second, working: 10 * time.Second},
		{name: "pausing twice keeps the first pause",
			steps: func(c *turnClock) {
				c.begin(t0)
				c.pause(at(10 * time.Second))
				c.pause(at(50 * time.Second))
				c.resume(at(time.Minute))
			},
			now: time.Minute, total: time.Minute, wait: 50 * time.Second, working: 10 * time.Second},
		{name: "two waits add up",
			steps: func(c *turnClock) {
				c.begin(t0)
				c.pause(at(time.Minute))
				c.resume(at(2 * time.Minute))
				c.pause(at(3 * time.Minute))
				c.resume(at(5 * time.Minute))
			},
			now: 6 * time.Minute, total: 6 * time.Minute, wait: 3 * time.Minute, working: 3 * time.Minute},
		{name: "resume without a pause is a no-op",
			steps: func(c *turnClock) { c.begin(t0); c.resume(at(time.Minute)) },
			now:   2 * time.Minute, total: 2 * time.Minute, working: 2 * time.Minute},
		{name: "no turn, no time", steps: func(c *turnClock) { c.pause(at(time.Minute)) },
			now: time.Hour},
		{name: "a new turn forgets the last one's wait",
			steps: func(c *turnClock) {
				c.begin(t0)
				c.pause(at(time.Second))
				c.begin(at(time.Hour))
			},
			now: time.Hour + 5*time.Second, total: 5 * time.Second, working: 5 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c turnClock
			tc.steps(&c)
			now := at(tc.now)
			if got := c.total(now); got != tc.total {
				t.Errorf("total = %v, want %v", got, tc.total)
			}
			if got := c.waiting(now); got != tc.wait {
				t.Errorf("waiting = %v, want %v", got, tc.wait)
			}
			if got := c.working(now); got != tc.working {
				t.Errorf("working = %v, want %v", got, tc.working)
			}
		})
	}
}

// The spinner read "thinking… 5h47m" after an AskUserQuestion had waited
// 5h41m for an answer: the turn's clock counted the user's time as the
// model's. In the model, a wait must pause the spinner's clock, and the end
// line must report the wait separately rather than inside "done in".
func TestTheTurnClockPausesWhileWaitingOnTheUser(t *testing.T) {
	now := time.Date(2026, 10, 22, 23, 29, 57, 0, time.UTC)
	m := newTestModel()
	m.resize(120, 40)
	m.now = func() time.Time { return now }
	m.setState(stateRunning)
	m.clock.begin(now)
	m.lastEventAt = now

	// The question is asked a minute in and answered 5h41m later.
	now = now.Add(time.Minute)
	m.lastEventAt = now
	m.setState(stateAwaitingAnswer)
	now = now.Add(5*time.Hour + 41*time.Minute)
	m.setState(stateRunning)

	view := stripANSI(m.bottomView())
	if strings.Contains(view, "5h") {
		t.Errorf("the spinner counts the wait as working time:\n%s", view)
	}
	if !strings.Contains(view, "1m0") {
		t.Errorf("the spinner should show about a minute of work:\n%s", view)
	}
	if strings.Contains(view, "quiet for") {
		t.Errorf("an answer after a long wait reads as a stalled model:\n%s", view)
	}

	m.update(doneMsg{res: agent.Result{StopReason: "end_turn", Text: "done"}})
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "done in 1m00s (+5h41m waiting for you)") {
		t.Errorf("end line should give the working time and the wait apart:\n%s", out)
	}
}

// Picker and confirm prompts are the user's own commands and can be open
// while the agent works; only the four states that block the turn pause it.
func TestOnlyBlockingStatesPauseTheClock(t *testing.T) {
	for s, want := range map[uiState]bool{
		stateAwaitingPermission: true, stateAwaitingAnswer: true, stateAnsweringOther: true, stateAwaitingPlan: true,
		stateAwaitingChoice: false, stateAwaitingConfirm: false, stateRunning: false, stateIdle: false,
	} {
		if got := waitsOnUser(s); got != want {
			t.Errorf("waitsOnUser(%v) = %v, want %v", s, got, want)
		}
	}
}

// An interruption reports working time too: interrupting a turn that sat on a
// question for an hour did not take an hour of the model's time.
func TestInterruptReportsWorkingTime(t *testing.T) {
	now := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	m := newTestModel()
	m.resize(100, 30)
	m.now = func() time.Time { return now }
	m.setState(stateRunning)
	m.clock.begin(now)
	now = now.Add(30 * time.Second)
	m.setState(stateAwaitingPermission)
	now = now.Add(time.Hour)
	m.turnCancel = func() {}
	m.interruptTurn()
	m.update(doneMsg{err: context.Canceled})
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "interrupted after 30.0s") {
		t.Errorf("want the working time in the interruption line:\n%s", out)
	}
}
