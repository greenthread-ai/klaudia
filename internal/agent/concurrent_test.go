package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// concTool is a fake tool used to observe concurrency. It reports the read-only
// name it impersonates (so readOnlyForConcurrency treats it as parallelizable
// or not) and records the peak number of Execute calls that were in flight
// simultaneously. Each Execute holds for a short window so overlapping calls are
// actually observable.
type concTool struct {
	name string

	mu        sync.Mutex
	active    int
	peak      int
	callOrder []string // input marker for each call, in the order Execute ran
}

func (c *concTool) Name() string { return c.name }
func (c *concTool) Description(context.Context) (string, error) {
	return "fake tool for concurrency tests", nil
}
func (c *concTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"marker":{"type":"string"}}}`)
}
func (c *concTool) ValidateInput(json.RawMessage) error { return nil }
func (c *concTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (c *concTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (c *concTool) Execute(_ context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	c.mu.Lock()
	c.active++
	if c.active > c.peak {
		c.peak = c.active
	}
	c.mu.Unlock()

	time.Sleep(30 * time.Millisecond)

	var in struct {
		Marker string `json:"marker"`
	}
	_ = json.Unmarshal(raw, &in)

	c.mu.Lock()
	c.active--
	c.callOrder = append(c.callOrder, in.Marker)
	c.mu.Unlock()

	// Echo the marker back so the caller can prove result order.
	return []tools.Result{{Content: "ran:" + in.Marker}}, nil
}

func (c *concTool) peakActive() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peak
}

// mutatingRecorder is a fake tool that shares one atomic "active" counter with a
// concTool so a test can prove it never overlaps the read-only executions. It
// classifies as non-read-only (its name is not in readOnlyForConcurrency), so
// the loop must run it sequentially.
type mutatingRecorder struct {
	name          string
	shared        *int32 // incremented by concTool executes; must be 0 when this runs
	sawConcurrent atomic.Bool
	order         *[]string
	orderMu       *sync.Mutex
}

func (m *mutatingRecorder) Name() string { return m.name }
func (m *mutatingRecorder) Description(context.Context) (string, error) {
	return "fake mutating tool", nil
}
func (m *mutatingRecorder) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"marker":{"type":"string"}}}`)
}
func (m *mutatingRecorder) ValidateInput(json.RawMessage) error { return nil }
func (m *mutatingRecorder) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (m *mutatingRecorder) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (m *mutatingRecorder) Execute(_ context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	if atomic.LoadInt32(m.shared) != 0 {
		m.sawConcurrent.Store(true)
	}
	time.Sleep(10 * time.Millisecond)
	if atomic.LoadInt32(m.shared) != 0 {
		m.sawConcurrent.Store(true)
	}
	var in struct {
		Marker string `json:"marker"`
	}
	_ = json.Unmarshal(raw, &in)
	m.orderMu.Lock()
	*m.order = append(*m.order, in.Marker)
	m.orderMu.Unlock()
	return []tools.Result{{Content: "ran:" + in.Marker}}, nil
}

func toolUse(id, name, marker string) anthropic.BetaToolUseBlock {
	raw, _ := json.Marshal(map[string]string{"marker": marker})
	return anthropic.BetaToolUseBlock{ID: id, Name: name, Input: json.RawMessage(raw)}
}

// resultOrder extracts the (tool_use_id, content) pairs from the dispatched
// result blocks, in order, by marshalling each block back to JSON.
func resultPairs(t *testing.T, blocks []anthropic.BetaContentBlockParamUnion) [][2]string {
	t.Helper()
	out := make([][2]string, 0, len(blocks))
	for _, b := range blocks {
		raw, _ := json.Marshal(b)
		// Content is either a plain string or an array of text/image blocks,
		// depending on the SDK helper used to build it. Handle both.
		var v struct {
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("unmarshal result block: %v (%s)", err, raw)
		}
		var content string
		if err := json.Unmarshal(v.Content, &content); err != nil {
			var arr []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(v.Content, &arr); err != nil {
				t.Fatalf("unmarshal content: %v (%s)", err, v.Content)
			}
			for _, blk := range arr {
				content += blk.Text
			}
		}
		out = append(out, [2]string{v.ToolUseID, content})
	}
	return out
}

func concOpts(proj string) Options {
	return Options{
		WorkingDir: proj,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver:   HeadlessApprover(false),
	}
}

// A run of consecutive read-only tool calls must execute concurrently: with the
// per-call Execute sleeping, the peak number of simultaneously-active executes
// must exceed 1. Run under -race.
func TestReadOnlyToolsRunConcurrently(t *testing.T) {
	proj := t.TempDir()
	ro := &concTool{name: "Read"}
	l := New(nil, tools.NewRegistry(ro))

	const n = 6
	uses := make([]anthropic.BetaToolUseBlock, n)
	for i := range uses {
		uses[i] = toolUse(fmt.Sprintf("t%d", i), "Read", fmt.Sprintf("m%d", i))
	}

	failures, streaks := map[string]int{}, map[string]errStreak{}
	blocks := l.dispatchToolUses(context.Background(), uses, "", false, 8192,
		concOpts(proj), nil, nil, failures, streaks)

	if peak := ro.peakActive(); peak < 2 {
		t.Fatalf("expected concurrent execution (peak active > 1), got peak=%d", peak)
	}

	// Result order must match tool_use order, id-for-id.
	pairs := resultPairs(t, blocks)
	if len(pairs) != n {
		t.Fatalf("got %d results, want %d", len(pairs), n)
	}
	for i, p := range pairs {
		wantID := fmt.Sprintf("t%d", i)
		wantContent := fmt.Sprintf("ran:m%d", i)
		if p[0] != wantID {
			t.Errorf("result %d: tool_use_id = %q, want %q", i, p[0], wantID)
		}
		if p[1] != wantContent {
			t.Errorf("result %d: content = %q, want %q", i, p[1], wantContent)
		}
	}
}

// A single read-only tool call must NOT be run through the concurrent path
// (group size must be >= 2), and its behavior/result must be identical to
// today's sequential dispatch. peak == 1 proves it never fanned out.
func TestSingleReadOnlyToolStaysSequential(t *testing.T) {
	proj := t.TempDir()
	ro := &concTool{name: "Read"}
	l := New(nil, tools.NewRegistry(ro))

	uses := []anthropic.BetaToolUseBlock{toolUse("only", "Read", "solo")}
	failures, streaks := map[string]int{}, map[string]errStreak{}
	blocks := l.dispatchToolUses(context.Background(), uses, "", false, 8192,
		concOpts(proj), nil, nil, failures, streaks)

	if peak := ro.peakActive(); peak != 1 {
		t.Fatalf("single call should have peak active == 1, got %d", peak)
	}
	pairs := resultPairs(t, blocks)
	if len(pairs) != 1 || pairs[0][0] != "only" || pairs[0][1] != "ran:solo" {
		t.Fatalf("unexpected result for single call: %v", pairs)
	}
}

// A mutating tool in the batch must run sequentially and in order: it must never
// overlap the read-only executions on either side of it, and result order must
// be preserved. Run under -race.
func TestMutatingToolBreaksTheConcurrentRun(t *testing.T) {
	proj := t.TempDir()

	var order []string
	var orderMu sync.Mutex

	// The read-only tool bumps a shared atomic while executing; the mutating
	// tool checks that counter is zero throughout its own execution.
	var active int32
	ro := &sharedActiveTool{name: "Grep", active: &active, order: &order, orderMu: &orderMu}
	mut := &mutatingRecorder{name: "Write", shared: &active, order: &order, orderMu: &orderMu}

	l := New(nil, tools.NewRegistry(ro, mut))

	// Layout: 3 read-only, 1 mutating, 3 read-only. The mutating tool splits the
	// batch into two concurrent groups and must itself run alone.
	uses := []anthropic.BetaToolUseBlock{
		toolUse("r0", "Grep", "r0"),
		toolUse("r1", "Grep", "r1"),
		toolUse("r2", "Grep", "r2"),
		toolUse("w0", "Write", "w0"),
		toolUse("r3", "Grep", "r3"),
		toolUse("r4", "Grep", "r4"),
		toolUse("r5", "Grep", "r5"),
	}
	wantIDs := []string{"r0", "r1", "r2", "w0", "r3", "r4", "r5"}

	failures, streaks := map[string]int{}, map[string]errStreak{}
	blocks := l.dispatchToolUses(context.Background(), uses, "", false, 8192,
		concOpts(proj), nil, nil, failures, streaks)

	if mut.sawConcurrent.Load() {
		t.Fatal("mutating tool overlapped a read-only execution; it must run alone")
	}
	if peak := ro.peakActive(); peak < 2 {
		t.Fatalf("read-only groups should run concurrently (peak > 1), got %d", peak)
	}

	pairs := resultPairs(t, blocks)
	if len(pairs) != len(wantIDs) {
		t.Fatalf("got %d results, want %d", len(pairs), len(wantIDs))
	}
	for i, p := range pairs {
		if p[0] != wantIDs[i] {
			t.Errorf("result %d: tool_use_id = %q, want %q", i, p[0], wantIDs[i])
		}
	}
}

// sharedActiveTool is a read-only fake that maintains a shared atomic "active"
// counter (so a mutating tool can detect overlap) plus a peak counter.
type sharedActiveTool struct {
	name    string
	active  *int32
	mu      sync.Mutex
	peak    int32
	order   *[]string
	orderMu *sync.Mutex
}

func (s *sharedActiveTool) Name() string { return s.name }
func (s *sharedActiveTool) Description(context.Context) (string, error) {
	return "fake read-only tool", nil
}
func (s *sharedActiveTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"marker":{"type":"string"}}}`)
}
func (s *sharedActiveTool) ValidateInput(json.RawMessage) error { return nil }
func (s *sharedActiveTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (s *sharedActiveTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (s *sharedActiveTool) Execute(_ context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	n := atomic.AddInt32(s.active, 1)
	s.mu.Lock()
	if n > s.peak {
		s.peak = n
	}
	s.mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	atomic.AddInt32(s.active, -1)
	var in struct {
		Marker string `json:"marker"`
	}
	_ = json.Unmarshal(raw, &in)
	return []tools.Result{{Content: "ran:" + in.Marker}}, nil
}

func (s *sharedActiveTool) peakActive() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}
