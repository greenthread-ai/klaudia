package tui

import (
	"fmt"
	"time"
)

// turnClock times a turn by the wall clock, and stops counting while the turn
// is waiting on the user.
//
// It replaces a bubbles stopwatch that misled in three ways. It kept running
// while a question sat unanswered, so after an AskUserQuestion left overnight
// the spinner read "thinking… 5h47m" — measured: 5h41m of that was the
// question, and the model's own answer took 40s. It counted ticks rather than
// time (each tick added its interval), so it under-reported whenever the UI
// was busy. And its Start and Stop were separate messages: the goal loop sent
// the next iteration's Start together with the previous turn's Stop, and when
// the Stop arrived second the clock was frozen for the whole iteration. A
// clock that is just arithmetic over time.Now has none of those failure
// modes; the spinner's own tick already drives the redraw.
type turnClock struct {
	start    time.Time
	pausedAt time.Time
	waited   time.Duration
}

// begin starts timing a turn, discarding whatever the last one left.
func (c *turnClock) begin(now time.Time) { *c = turnClock{start: now} }

// pause stops the working time while the turn waits on the user. Pausing
// twice, or with no turn running, does nothing.
func (c *turnClock) pause(now time.Time) {
	if !c.start.IsZero() && c.pausedAt.IsZero() {
		c.pausedAt = now
	}
}

// resume restarts the working time; the paused span is counted as waiting.
func (c *turnClock) resume(now time.Time) {
	if !c.pausedAt.IsZero() {
		c.waited += now.Sub(c.pausedAt)
		c.pausedAt = time.Time{}
	}
}

// total is the turn's wall-clock length so far, waiting included.
func (c turnClock) total(now time.Time) time.Duration {
	if c.start.IsZero() {
		return 0
	}
	return now.Sub(c.start)
}

// waiting is how long the turn has spent waiting on the user, including a wait
// still in progress.
func (c turnClock) waiting(now time.Time) time.Duration {
	w := c.waited
	if !c.pausedAt.IsZero() {
		w += now.Sub(c.pausedAt)
	}
	return w
}

// working is the turn's length less the time spent waiting on the user: what
// the spinner shows and what "done in" reports.
func (c turnClock) working(now time.Time) time.Duration {
	return c.total(now) - c.waiting(now)
}

// waitsOnUser reports whether s is the turn blocked on the user's answer.
//
// Only these four. A picker or a confirm can be open while the agent keeps
// working (settleState hands the state back afterwards), and those are the
// user's own commands, not the turn waiting on them.
func waitsOnUser(s uiState) bool {
	switch s {
	case stateAwaitingPermission, stateAwaitingAnswer, stateAnsweringOther, stateAwaitingPlan:
		return true
	}
	return false
}

// liveElapsed formats the running clock the way the stopwatch did, to a tenth
// of a second, so the spinner line keeps its look.
func liveElapsed(d time.Duration) string {
	return d.Truncate(100 * time.Millisecond).String()
}

// waitNote is the suffix for a turn's end line when it waited on the user long
// enough to matter. A few seconds on an approval is noise; a minute is the
// kind of gap that made a turn look stuck.
func waitNote(waited time.Duration) string {
	if waited < time.Minute {
		return ""
	}
	return fmt.Sprintf(" (+%s waiting for you)", fmtDuration(waited))
}
