package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Background runs sub-agents past the turn that started them — the Agent
// tool's run_in_background — and tells its owner when each one finishes.
//
// It implements tools.BackgroundRunner and is frontend-neutral: the owner
// supplies ctx (a session's lifetime, so interrupting a turn does not kill a
// research task the user asked for) and onDone (how a result reaches the
// model — the TUI queues it as a steering message, or starts a turn when
// idle). Everything it knows is in List, for whatever shows running work.
type Background struct {
	ctx    context.Context
	onDone func(BackgroundTask)

	mu    sync.Mutex
	seq   int
	tasks []*bgTask
}

// BackgroundTask is a snapshot of one background run.
type BackgroundTask struct {
	ID      string
	Label   string // "Explore: survey the MCP code"
	Started time.Time
	Ended   time.Time // zero while running
	Running bool
	Last    string // the most recent progress line
	Result  string
	Err     error
}

type bgTask struct {
	BackgroundTask
	cancel context.CancelFunc
}

// NewBackground returns a runner whose work lives as long as ctx. onDone is
// called once per task, from the task's goroutine, after it has finished.
func NewBackground(ctx context.Context, onDone func(BackgroundTask)) *Background {
	return &Background{ctx: ctx, onDone: onDone}
}

// Start runs work and returns its id ("agent_1", …) at once.
func (b *Background) Start(label string, work func(ctx context.Context, progress func(string)) (string, error)) string {
	ctx, cancel := context.WithCancel(b.ctx)
	b.mu.Lock()
	b.seq++
	t := &bgTask{BackgroundTask: BackgroundTask{
		ID: fmt.Sprintf("agent_%d", b.seq), Label: label, Started: time.Now(), Running: true,
	}, cancel: cancel}
	b.tasks = append(b.tasks, t)
	b.mu.Unlock()

	go func() {
		defer cancel()
		result, err := work(ctx, func(line string) {
			b.mu.Lock()
			t.Last = line
			b.mu.Unlock()
		})
		b.mu.Lock()
		t.Running, t.Ended, t.Result, t.Err = false, time.Now(), result, err
		if err == nil && ctx.Err() != nil {
			t.Err = ctx.Err() // stopped, though the work returned cleanly
		}
		snap := t.BackgroundTask
		b.mu.Unlock()
		if b.onDone != nil {
			b.onDone(snap)
		}
	}()
	return t.ID
}

// List returns every task this runner has started, oldest first.
func (b *Background) List() []BackgroundTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]BackgroundTask, len(b.tasks))
	for i, t := range b.tasks {
		out[i] = t.BackgroundTask
	}
	return out
}

// Running counts the tasks still going.
func (b *Background) Running() int {
	n := 0
	for _, t := range b.List() {
		if t.Running {
			n++
		}
	}
	return n
}

// Stop cancels a running task by id; onDone still fires, with the error.
func (b *Background) Stop(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, t := range b.tasks {
		if t.ID == id && t.Running {
			t.cancel()
			return true
		}
	}
	return false
}

// Notice is how a finished task is reported to the model: what ran, how it
// ended, and its result in full.
func (t BackgroundTask) Notice() string {
	status := "completed"
	body := t.Result
	if t.Err != nil {
		status = "failed"
		if t.Err == context.Canceled {
			status = "stopped"
		}
		body = t.Err.Error()
		if t.Result != "" {
			body += "\n\n" + t.Result
		}
	}
	return fmt.Sprintf("<background-agent id=%q status=%q>\nBackground sub-agent %s (%s) %s after %s.\n\n%s\n</background-agent>",
		t.ID, status, t.ID, t.Label, status, t.Ended.Sub(t.Started).Round(time.Second), body)
}
