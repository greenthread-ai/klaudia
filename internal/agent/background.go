package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Background sub-agents.
//
// The synchronous Agent tool blocks the parent turn until the child returns —
// right for a quick fan-out, wrong for a twenty-minute investigation the user
// would rather not sit behind. A background sub-agent returns a handle at once;
// the parent keeps working, and the child's result is delivered on a later turn
// (CollectBackground, polled by the loop) and listed live by the /agents view.
//
// This mirrors how an async agent is reported here: launched, tracked by id and
// status, and surfaced back when it finishes rather than awaited inline.

// BackgroundStatus is where a background sub-agent is in its life.
type BackgroundStatus string

const (
	// BackgroundRunning: the child loop is still going.
	BackgroundRunning BackgroundStatus = "running"
	// BackgroundSucceeded: it returned a result.
	BackgroundSucceeded BackgroundStatus = "succeeded"
	// BackgroundFailed: it errored (spawn, worktree, or the loop itself).
	BackgroundFailed BackgroundStatus = "failed"
)

// BackgroundAgent is a snapshot of one background sub-agent, safe to hand to a
// frontend: it is a copy, so reading it never races the registry's writers.
type BackgroundAgent struct {
	ID         string
	Type       string
	Label      string // the task description the model gave, for the status view
	Status     BackgroundStatus
	StartedAt  time.Time
	FinishedAt time.Time // zero while running
	Activity   string    // last tool the child ran, for a live status line
	Result     string    // final text, once succeeded
	Err        string    // error text, once failed
	Isolated   bool      // ran in its own git worktree (a writer) vs shared tree
}

// Elapsed is how long the agent has run: to completion if finished, else so far.
func (a BackgroundAgent) Elapsed() time.Duration {
	end := a.FinishedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(a.StartedAt)
}

// Done reports whether the agent has stopped running.
func (a BackgroundAgent) Done() bool { return a.Status != BackgroundRunning }

// BackgroundRegistry tracks background sub-agents launched from one session.
//
// It is shared between the goroutines running the children, the Agent tool that
// launches them, the loop that collects their results, and the /agents view
// that lists them — so every access is under the mutex, and List/Get/Snapshot
// hand back copies rather than the live pointers.
type BackgroundRegistry struct {
	mu        sync.Mutex
	seq       int
	byID      map[string]*backgroundEntry
	order     []string
	collected map[string]bool // ids whose result has been delivered to the parent
	clock     func() time.Time
}

// backgroundEntry is the registry's mutable record; BackgroundAgent is its
// snapshot. cancel stops the child's context (wired by SpawnBackground).
type backgroundEntry struct {
	agent  BackgroundAgent
	cancel func()
}

// NewBackgroundRegistry builds an empty registry.
func NewBackgroundRegistry() *BackgroundRegistry {
	return &BackgroundRegistry{
		byID:      map[string]*backgroundEntry{},
		collected: map[string]bool{},
		clock:     time.Now,
	}
}

func (r *BackgroundRegistry) now() time.Time {
	if r.clock != nil {
		return r.clock()
	}
	return time.Now()
}

// register adds a running agent and returns its assigned id. cancel is stored so
// the entry can be stopped later; it may be nil.
func (r *BackgroundRegistry) register(subagentType, label string, isolated bool, cancel func()) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	id := fmt.Sprintf("agent-%d", r.seq)
	r.byID[id] = &backgroundEntry{
		agent: BackgroundAgent{
			ID:        id,
			Type:      subagentType,
			Label:     label,
			Status:    BackgroundRunning,
			StartedAt: r.now(),
			Isolated:  isolated,
		},
		cancel: cancel,
	}
	r.order = append(r.order, id)
	return id
}

// setActivity records the child's most recent tool call for the live view.
func (r *BackgroundRegistry) setActivity(id, activity string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.byID[id]; ok {
		e.agent.Activity = activity
	}
}

// finish records a terminal outcome. A non-nil err marks the agent failed even
// when result is non-empty (a partial answer plus an error is still a failure).
func (r *BackgroundRegistry) finish(id, result string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byID[id]
	if !ok {
		return
	}
	e.agent.FinishedAt = r.now()
	if err != nil {
		e.agent.Status = BackgroundFailed
		e.agent.Err = err.Error()
		e.agent.Result = result
		return
	}
	e.agent.Status = BackgroundSucceeded
	e.agent.Result = result
}

// Get returns a snapshot of one agent.
func (r *BackgroundRegistry) Get(id string) (BackgroundAgent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.byID[id]
	if !ok {
		return BackgroundAgent{}, false
	}
	return e.agent, true
}

// List returns snapshots of every agent in launch order.
func (r *BackgroundRegistry) List() []BackgroundAgent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]BackgroundAgent, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id].agent)
	}
	return out
}

// Cancel stops a running agent's context. Returns false if it is unknown or
// already finished.
func (r *BackgroundRegistry) Cancel(id string) bool {
	r.mu.Lock()
	e, ok := r.byID[id]
	if !ok || e.agent.Done() || e.cancel == nil {
		r.mu.Unlock()
		return false
	}
	cancel := e.cancel
	r.mu.Unlock()
	cancel()
	return true
}

// TakeFinished returns the finished agents not yet collected, marking them
// collected so each result is delivered to the parent exactly once. The loop
// polls this between turns; the /agents view uses List instead so it keeps
// showing finished agents.
func (r *BackgroundRegistry) TakeFinished() []BackgroundAgent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []BackgroundAgent
	// Deliver in the order they were launched, for a stable report.
	ids := append([]string(nil), r.order...)
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range r.order {
		e := r.byID[id]
		if e.agent.Done() && !r.collected[id] {
			r.collected[id] = true
			out = append(out, e.agent)
		}
	}
	return out
}

// PendingReport formats the newly-finished background agents as a user message
// for the parent loop, or "" when none are pending. Wiring this to
// Options.CollectBackground is what turns "launched in the background" into
// "delivered when ready".
func (r *BackgroundRegistry) PendingReport() string {
	finished := r.TakeFinished()
	if len(finished) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Background sub-agent(s) you launched earlier have finished. " +
		"Their results are below; fold anything useful into your work.\n")
	for _, a := range finished {
		fmt.Fprintf(&b, "\n[%s · %s · %s", a.ID, a.Type, a.Status)
		if a.Label != "" {
			fmt.Fprintf(&b, " · %s", a.Label)
		}
		b.WriteString("]\n")
		switch a.Status {
		case BackgroundFailed:
			b.WriteString("failed: " + a.Err + "\n")
			if a.Result != "" {
				b.WriteString(a.Result + "\n")
			}
		default:
			if a.Result != "" {
				b.WriteString(a.Result + "\n")
			} else {
				b.WriteString("(no output)\n")
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// pollBackground reads any finished-agent report, if the caller wired a source.
func pollBackground(opts Options) string {
	if opts.CollectBackground == nil {
		return ""
	}
	return opts.CollectBackground()
}

// backgroundMessage renders a finished-agent report as a user message.
func backgroundMessage(report string) (anthropic.BetaMessageParam, bool) {
	if strings.TrimSpace(report) == "" {
		return anthropic.BetaMessageParam{}, false
	}
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(report)), true
}
