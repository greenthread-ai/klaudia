package agent

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/permission"
	tools "github.com/greenthread-ai/klaudia/pkg/tool"
)

// maxConcurrentTools caps how many tool calls execute at once.
//
// Five, matching what the harnesses that do this have converged on. The number
// is a guess bounded by two real costs rather than a measurement: file
// descriptors and page cache are not the constraint, but the frontend is — a
// group that finishes together delivers its buffered events together, and a
// wide group turns the transcript into a wall. Raising it is a measurement, not
// an edit.
const maxConcurrentTools = 5

// dispatchAll runs a turn's tool calls and returns their results in call
// order.
//
// Runs of adjacent concurrency-safe calls execute together; everything else
// runs one at a time, exactly as before. The adjacency requirement is the
// important part: a Read, an Edit and another Read must stay in that order,
// because the second Read's result depends on the Edit having happened. Only a
// contiguous run can be collapsed without reordering anything, so the groups
// are maximal runs and nothing is ever lifted past a call it follows.
//
// The win is latency on the shape that dominates a turn — three or four Reads,
// or a Glob and two Greps, issued together to orient. Serially those are the
// sum of their round trips; grouped they are the slowest one.
func (l *Loop) dispatchAll(
	ctx context.Context,
	toolUses []anthropic.BetaToolUseBlock,
	opts Options,
	emit Emitter,
	reveal func(...string),
	fs *failureState,
	preempt func(anthropic.BetaToolUseBlock) (string, bool),
) []anthropic.BetaContentBlockParamUnion {
	blocks := make([]anthropic.BetaContentBlockParamUnion, len(toolUses))
	for start := 0; start < len(toolUses); {
		end := start + 1
		if l.groupable(toolUses[start], opts, preempt) {
			for end < len(toolUses) && l.groupable(toolUses[end], opts, preempt) {
				end++
			}
		}
		if end-start == 1 {
			tu := toolUses[start]
			if msg, ok := preempt(tu); ok {
				blocks[start] = shortCircuit(emit, tu, msg)
			} else {
				blocks[start] = l.dispatch(ctx, tu, opts, emit, reveal, fs)
			}
			start = end
			continue
		}
		l.dispatchGroup(ctx, toolUses[start:end], blocks[start:end], opts, emit, reveal, fs)
		start = end
	}
	return blocks
}

// groupable reports whether a call may run alongside its neighbours.
//
// Two conditions, and the second is the one that is easy to miss. The tool has
// to have opted in (tools.ConcurrencySafe), and this specific call must not be
// about to prompt: the host gate and the permission check both route through a
// single Approver with one place to put a question, so two calls arriving at it
// together would race for the user's attention and at best queue invisibly.
// Deciding that here, before anything starts, is what keeps approval strictly
// serial without dispatch needing to know it is in a group.
//
// Both checks are side-effect free — Gate.WouldAsk decides without asking or
// recording — so running them here and again inside dispatch costs a
// classification and changes nothing.
func (l *Loop) groupable(tu anthropic.BetaToolUseBlock, opts Options, preempt func(anthropic.BetaToolUseBlock) (string, bool)) bool {
	if _, ok := preempt(tu); ok {
		return false
	}
	tool, ok := l.tools.Lookup(tu.Name)
	if !ok || !tools.IsConcurrencySafe(tool) {
		return false
	}
	raw, _ := json.Marshal(tu.Input)
	if opts.Gate != nil && permission.CurrentMode(opts.Permission) != permission.ModeBypassPermissions {
		if opts.Gate.WouldAsk(tu.Name, raw, opts.WorkingDir) {
			return false
		}
	}
	return permission.Check(opts.Permission, tool, tool.PermissionRequest(raw)).Behavior != permission.Ask
}

// dispatchGroup executes a run of calls concurrently, writing each result to
// its own slot in out. Distinct elements of a pre-sized slice, so the slice
// itself is never written and the results come back in call order regardless
// of which call finished first.
func (l *Loop) dispatchGroup(
	ctx context.Context,
	group []anthropic.BetaToolUseBlock,
	out []anthropic.BetaContentBlockParamUnion,
	opts Options,
	emit Emitter,
	reveal func(...string),
	fs *failureState,
) {
	oe := newOrderedEmitter(emit, len(group))
	sem := make(chan struct{}, maxConcurrentTools)
	var wg sync.WaitGroup
	for i, tu := range group {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, tu anthropic.BetaToolUseBlock) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = l.dispatch(ctx, tu, opts, oe.emitter(i), reveal, fs)
			oe.done(i)
		}(i, tu)
	}
	wg.Wait()
}

// orderedEmitter serializes a group's events and delivers them in call order,
// so a parallel batch reads in the transcript exactly as a serial one would.
//
// Without it the frontend would interleave: three Reads emit tool_use,
// tool_use, tool_result, tool_use, tool_result… in whatever order the
// scheduler picked, and the pairing a reader relies on is gone. Sorting
// afterwards is not an option either, because events are consumed as they
// arrive.
//
// The call at the head of the group streams live — its events go straight
// through, so the user sees movement immediately rather than nothing until the
// whole group lands. Every later call buffers until the calls before it have
// finished, at which point its buffer is flushed and it becomes the head. A
// group of one therefore behaves exactly like the serial path.
//
// The mutex also makes this the only goroutine calling the underlying Emitter
// at a time, which is what lets frontends keep assuming a single producer.
type orderedEmitter struct {
	mu       sync.Mutex
	base     Emitter
	next     int
	bufs     [][]Event
	finished []bool
}

func newOrderedEmitter(base Emitter, n int) *orderedEmitter {
	return &orderedEmitter{base: base, bufs: make([][]Event, n), finished: make([]bool, n)}
}

// emitter returns the Emitter call i should use. Nil when there is no
// listener, so dispatch's "is anyone watching?" checks still skip the work of
// building events nobody will read.
func (o *orderedEmitter) emitter(i int) Emitter {
	if o.base == nil {
		return nil
	}
	return func(ev Event) {
		o.mu.Lock()
		defer o.mu.Unlock()
		if i == o.next {
			o.base(ev)
			return
		}
		o.bufs[i] = append(o.bufs[i], ev)
	}
}

// done marks call i complete and flushes whatever is now at the head.
func (o *orderedEmitter) done(i int) {
	if o.base == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finished[i] = true
	for o.next < len(o.bufs) && o.finished[o.next] {
		for _, ev := range o.bufs[o.next] {
			o.base(ev)
		}
		o.bufs[o.next] = nil
		o.next++
	}
}

// failureState holds the two loop-breaker counters behind a mutex, because a
// group's calls now touch them at the same time.
//
// What the lock does and does not buy: each operation is atomic, so the maps
// cannot tear. It does not make a breaker *decision* atomic with the update
// that follows it, and deliberately so — serialising dispatch around the
// counters would undo the parallelism to protect a heuristic. The consequence
// is bounded and benign: calls in one group see the same counts, so a group can
// run one extra copy of a call that was about to be refused. The breaker
// engages on the next turn as before. Only concurrency-safe tools can be
// grouped, so the extra copy is a read that reads again.
type failureState struct {
	mu sync.Mutex
	// failures counts how many times each IDENTICAL call (name+input) has
	// failed within this Run.
	failures map[string]int
	// streaks counts consecutive failures of the same SHAPE per tool,
	// regardless of input.
	streaks map[string]errStreak
}

func newFailureState() *failureState {
	return &failureState{failures: map[string]int{}, streaks: map[string]errStreak{}}
}

func (f *failureState) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failures[key]
}

func (f *failureState) fail(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[key]++
}

func (f *failureState) streak(tool string) errStreak {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streaks[tool]
}

func (f *failureState) bumpStreak(tool, msg, input string, preExec bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	bumpErrStreak(f.streaks, tool, msg, input, preExec)
}

func (f *failureState) clearStreak(tool string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.streaks, tool)
}

// clear resets both counters after something that makes them stale: a clean
// run, or an approval that supersedes earlier refusals.
func (f *failureState) clear(key, tool string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failures, key)
	delete(f.streaks, tool)
}

// revealSet is the set of deferred tools ToolSearch has made active.
//
// ToolSearch is not concurrency-safe, so nothing grouped can reveal anything
// today. It is guarded anyway: reveal is handed to every tool through
// tools.Context, and a tool added later that both runs in a group and reveals
// would otherwise corrupt the map with no sign of why.
type revealSet struct {
	mu sync.Mutex
	m  map[string]bool
}

func newRevealSet() *revealSet { return &revealSet{m: map[string]bool{}} }

func (r *revealSet) add(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range names {
		r.m[n] = true
	}
}

// snapshot copies the set for the request builder, which reads it between
// turns. Copying keeps buildToolParams taking a plain map.
func (r *revealSet) snapshot() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]bool, len(r.m))
	for n := range r.m {
		out[n] = true
	}
	return out
}
