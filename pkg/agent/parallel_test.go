package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// probeTool is a tool whose concurrency, permission behaviour and execution
// body are all set per test, and which records when it entered and left
// Execute. The recording is what makes "did these actually overlap?" an
// assertion rather than a timing guess.
type probeTool struct {
	name string
	safe bool
	ask  bool
	run  func(ctx context.Context, id string) (string, bool)
	log  *runLog
}

func (p *probeTool) Name() string                                { return p.name }
func (p *probeTool) Description(context.Context) (string, error) { return "", nil }
func (p *probeTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}}}`)
}
func (p *probeTool) ValidateInput(json.RawMessage) error { return nil }
func (p *probeTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (p *probeTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	if p.ask {
		return permission.Decision{Behavior: permission.Ask}
	}
	return permission.Decision{Behavior: permission.Allow}
}
func (p *probeTool) ConcurrencySafe() bool { return p.safe }

func (p *probeTool) Execute(ctx context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	var in struct{ ID string }
	_ = json.Unmarshal(raw, &in)
	p.log.enter(p.name + ":" + in.ID)
	defer p.log.leave(p.name + ":" + in.ID)
	out, isErr := "ok", false
	if p.run != nil {
		out, isErr = p.run(ctx, in.ID)
	}
	return []tools.Result{{Content: out, IsError: isErr}}, nil
}

// runLog records overlap and arrival order across goroutines.
type runLog struct {
	mu      sync.Mutex
	live    int
	peak    int
	entered []string
}

func (r *runLog) enter(tag string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live++
	if r.live > r.peak {
		r.peak = r.live
	}
	r.entered = append(r.entered, tag)
}

func (r *runLog) leave(string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live--
}

func (r *runLog) peakConcurrency() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.peak
}

func (r *runLog) order() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.entered...)
}

// barrier blocks every arrival until n of them are present. Run serially the
// first arrival waits alone and times out, which is how these tests
// distinguish "ran at the same time" from "ran fast" without sleeping and
// hoping.
type barrier struct {
	mu    sync.Mutex
	n     int
	count int
	ready chan struct{}
}

func newBarrier(n int) *barrier {
	return &barrier{n: n, ready: make(chan struct{})}
}

func (b *barrier) arrive(timeout time.Duration) error {
	b.mu.Lock()
	b.count++
	if b.count == b.n {
		close(b.ready)
	}
	b.mu.Unlock()
	select {
	case <-b.ready:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("only %d of %d calls arrived together", b.count, b.n)
	}
}

// callsTo builds a tool_use per name, with stable ids t1..tn so a test can
// assert on ordering.
func callsTo(names ...string) []anthropic.BetaToolUseBlock {
	out := make([]anthropic.BetaToolUseBlock, len(names))
	for i, n := range names {
		id := fmt.Sprintf("t%d", i+1)
		out[i] = anthropic.BetaToolUseBlock{ID: id, Name: n, Input: map[string]any{"id": id}}
	}
	return out
}

func noPreempt(anthropic.BetaToolUseBlock) (string, bool) { return "", false }

func runBatch(t *testing.T, reg *tools.Registry, opts Options, emit Emitter, names ...string) []string {
	t.Helper()
	l := New(nil, reg)
	blocks := l.dispatchAll(context.Background(), callsTo(names...), opts, emit,
		func(...string) {}, newFailureState(), noPreempt)
	got := make([]string, len(blocks))
	for i, b := range blocks {
		got[i] = toolResultText(t, b)
	}
	return got
}

// The headline behaviour: adjacent concurrency-safe calls execute together.
// The barrier makes it unambiguous — serially, each call waits for two
// siblings that cannot arrive until it returns, so all three time out.
func TestConcurrencySafeToolsRunTogether(t *testing.T) {
	b := newBarrier(3)
	log := &runLog{}
	probe := &probeTool{name: "Peek", safe: true, log: log, run: func(context.Context, string) (string, bool) {
		if err := b.arrive(2 * time.Second); err != nil {
			return err.Error(), true
		}
		return "ok", false
	}}

	got := runBatch(t, tools.NewRegistry(probe), Options{}, nil, "Peek", "Peek", "Peek")
	for i, g := range got {
		if g != "ok" {
			t.Errorf("call %d did not run concurrently: %s", i, g)
		}
	}
	if peak := log.peakConcurrency(); peak != 3 {
		t.Errorf("peak concurrency = %d, want 3", peak)
	}
}

// Opting in is the whole contract. A tool that has not implemented
// ConcurrencySafe keeps the old one-at-a-time behaviour, because most tools
// were written on that assumption and adding a tool should not require
// thinking about parallelism.
func TestUnmarkedToolsStaySerial(t *testing.T) {
	log := &runLog{}
	probe := &probeTool{name: "Mutate", safe: false, log: log, run: func(context.Context, string) (string, bool) {
		time.Sleep(5 * time.Millisecond)
		return "ok", false
	}}

	runBatch(t, tools.NewRegistry(probe), Options{}, nil, "Mutate", "Mutate", "Mutate")
	if peak := log.peakConcurrency(); peak != 1 {
		t.Errorf("peak concurrency = %d, want 1: an unmarked tool ran beside a sibling", peak)
	}
}

// Results are indexed, not appended, so the model sees them paired with the
// calls it made however the goroutines finished. Here the last call finishes
// first.
func TestResultsComeBackInCallOrder(t *testing.T) {
	log := &runLog{}
	probe := &probeTool{name: "Peek", safe: true, log: log, run: func(_ context.Context, id string) (string, bool) {
		switch id {
		case "t1":
			time.Sleep(30 * time.Millisecond)
		case "t2":
			time.Sleep(15 * time.Millisecond)
		}
		return id, false
	}}

	got := runBatch(t, tools.NewRegistry(probe), Options{}, nil, "Peek", "Peek", "Peek")
	for i, want := range []string{"t1", "t2", "t3"} {
		if got[i] != want {
			t.Errorf("result %d = %q, want %q", i, got[i], want)
		}
	}
}

// Only *adjacent* safe calls are grouped. A mutating call between two reads is
// a barrier: the reads after it may depend on it having happened, and lifting
// one past it would change what they see. Nothing is reordered — the groups are
// maximal runs.
func TestAMutatingCallSplitsTheGroup(t *testing.T) {
	log := &runLog{}
	reg := tools.NewRegistry(
		&probeTool{name: "Peek", safe: true, log: log},
		&probeTool{name: "Mutate", safe: false, log: log, run: func(context.Context, string) (string, bool) {
			if peak := log.peakConcurrency(); peak > 2 {
				return fmt.Sprintf("ran with %d others live", peak), true
			}
			return "ok", false
		}},
	)

	runBatch(t, reg, Options{}, nil, "Peek", "Peek", "Mutate", "Peek", "Peek")

	order := log.order()
	if len(order) != 5 {
		t.Fatalf("got %d executions, want 5: %v", len(order), order)
	}
	// The first two are a group and may arrive in either order; the mutating
	// call must come third, and the last two only after it.
	if order[2] != "Mutate:t3" {
		t.Errorf("execution order = %v, want the mutating call third", order)
	}
	for _, i := range []int{0, 1} {
		if order[i] == "Mutate:t3" {
			t.Errorf("the mutating call was lifted past a read it followed: %v", order)
		}
	}
}

// A group's events are delivered in call order, so the transcript of a
// parallel batch reads exactly like a serial one. Without this the frontend
// interleaves tool_use/tool_result pairs by completion time and the pairing a
// reader relies on is gone.
func TestGroupEventsArriveInCallOrder(t *testing.T) {
	log := &runLog{}
	probe := &probeTool{name: "Peek", safe: true, log: log, run: func(_ context.Context, id string) (string, bool) {
		if id == "t1" {
			time.Sleep(30 * time.Millisecond)
		}
		return id, false
	}}

	var mu sync.Mutex
	var seen []string
	emit := func(ev Event) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, ev.Type+":"+ev.ToolUseID)
	}

	runBatch(t, tools.NewRegistry(probe), Options{}, emit, "Peek", "Peek", "Peek")

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"tool_use:t1", "tool_result:t1",
		"tool_use:t2", "tool_result:t2",
		"tool_use:t3", "tool_result:t3",
	}
	if len(seen) != len(want) {
		t.Fatalf("events = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("events = %v, want %v", seen, want)
		}
	}
}

// A wide batch is throttled rather than spawning a goroutine per call.
func TestConcurrencyIsCapped(t *testing.T) {
	const calls = maxConcurrentTools + 4
	log := &runLog{}
	probe := &probeTool{name: "Peek", safe: true, log: log, run: func(context.Context, string) (string, bool) {
		time.Sleep(10 * time.Millisecond)
		return "ok", false
	}}

	names := make([]string, calls)
	for i := range names {
		names[i] = "Peek"
	}
	runBatch(t, tools.NewRegistry(probe), Options{}, nil, names...)

	peak := log.peakConcurrency()
	if peak > maxConcurrentTools {
		t.Errorf("peak concurrency = %d, want at most %d", peak, maxConcurrentTools)
	}
	if peak < 2 {
		t.Errorf("peak concurrency = %d: nothing ran in parallel", peak)
	}
}

// The condition that is easy to miss: a call that is about to ask the user
// cannot be grouped, however read-only the tool is. There is one Approver and
// one place to put a question, so two prompts at once either race for the
// user's attention or queue where nobody can see them.
func TestACallThatWouldPromptIsNotGrouped(t *testing.T) {
	log := &runLog{}
	probe := &probeTool{name: "Peek", safe: true, ask: true, log: log}

	var mu sync.Mutex
	live, peakAsks := 0, 0
	opts := Options{Approver: ApproverFunc(func(context.Context, ApprovalRequest) permission.Decision {
		mu.Lock()
		live++
		if live > peakAsks {
			peakAsks = live
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		mu.Lock()
		live--
		mu.Unlock()
		return permission.Decision{Behavior: permission.Allow}
	})}

	runBatch(t, tools.NewRegistry(probe), opts, nil, "Peek", "Peek", "Peek")

	mu.Lock()
	defer mu.Unlock()
	if peakAsks != 1 {
		t.Errorf("%d approval prompts were open at once, want 1", peakAsks)
	}
	if peak := log.peakConcurrency(); peak != 1 {
		t.Errorf("peak concurrency = %d, want 1: a prompting call was grouped", peak)
	}
}

// The loop-breaker counters are shared mutable state that every call in a
// group touches. Run with -race; before failureState this was an
// unsynchronised map write, which the Go runtime turns into a fatal error
// rather than a flake.
//
// It also pins down what the counters still do under concurrency. The breaker
// engages inside the batch, exactly as it does serially: a few calls fail for
// real and the rest are refused with the directive. What the lock does not
// promise is that the decision and the update are atomic, so the number that
// get through before it engages may vary by one — hence the assertion is on
// the shape of the outcome, not an exact split.
func TestGroupedFailuresDoNotRaceTheCounters(t *testing.T) {
	probe := &probeTool{name: "Peek", safe: true, log: &runLog{}, run: func(context.Context, string) (string, bool) {
		return "it failed", true
	}}

	l := New(nil, tools.NewRegistry(probe))
	fs := newFailureState()
	names := make([]string, maxConcurrentTools+3)
	for i := range names {
		names[i] = "Peek"
	}
	blocks := l.dispatchAll(context.Background(), callsTo(names...), Options{}, nil,
		func(...string) {}, fs, noPreempt)

	ran, refused := 0, 0
	for i, b := range blocks {
		switch got := toolResultText(t, b); {
		case got == "it failed":
			ran++
		case strings.Contains(got, "in a row"):
			refused++
		default:
			t.Errorf("result %d is neither a failure nor a directive: %q", i, got)
		}
	}
	if ran < repeatFailureLimit {
		t.Errorf("%d calls actually ran, want at least %d before the breaker engaged", ran, repeatFailureLimit)
	}
	if refused == 0 {
		t.Error("the breaker never engaged, so the counters were not shared across the group")
	}
}

// A tool_use the model never finished emitting is refused without running, and
// that must hold whether or not the tool is concurrency-safe: a truncated call
// has empty input, so a grouped one would run with no arguments.
func TestATruncatedCallIsNotGrouped(t *testing.T) {
	log := &runLog{}
	probe := &probeTool{name: "Peek", safe: true, log: log}
	l := New(nil, tools.NewRegistry(probe))

	calls := callsTo("Peek", "Peek", "Peek")
	preempt := func(tu anthropic.BetaToolUseBlock) (string, bool) {
		if tu.ID == "t2" {
			return "cut off", true
		}
		return "", false
	}
	blocks := l.dispatchAll(context.Background(), calls, Options{}, nil,
		func(...string) {}, newFailureState(), preempt)

	if got := toolResultText(t, blocks[1]); got != "cut off" {
		t.Errorf("the truncated call was not refused: %q", got)
	}
	for _, tag := range log.order() {
		if tag == "Peek:t2" {
			t.Error("the truncated call ran")
		}
	}
}
