package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// fakeWorktrees records the ids it isolated and whether each was cleaned up.
type fakeWorktrees struct {
	mu       sync.Mutex
	created  []string
	cleaned  []string
	dir      string
	failWith error
}

func (f *fakeWorktrees) Create(_ context.Context, _, id string) (string, func() error, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return "", nil, f.failWith
	}
	f.created = append(f.created, id)
	dir := f.dir
	if dir == "" {
		dir = "/tmp/fake-worktree/" + id
	}
	cleanup := func() error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.cleaned = append(f.cleaned, id)
		return nil
	}
	return dir, cleanup, nil
}

func (f *fakeWorktrees) createdIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.created...)
}

func (f *fakeWorktrees) cleanedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cleaned...)
}

// waitFor polls cond until true or the deadline, so a test never blocks forever
// on a background goroutine that misbehaves.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func backgroundSpawner(t *testing.T, provider api.Provider, dir string, wt WorktreeProvider) *Spawner {
	t.Helper()
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	write, err := tools.NewWrite()
	if err != nil {
		t.Fatal(err)
	}
	return NewSpawner(provider, tools.NewRegistry(read, write), "claude-opus-4-8",
		bypassPerm(), nil, 0).WithWorkingDir(dir).WithWorktrees(wt)
}

// A background launch must return a handle promptly and not block on the child.
func TestSpawnBackgroundReturnsHandleImmediately(t *testing.T) {
	dir, path := fixtureFile(t)
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu1", "Read", map[string]any{"file_path": path}),
	}}
	s := backgroundSpawner(t, provider, dir, &fakeWorktrees{})

	id, err := s.SpawnBackground("Explore", "read it", "read the notes", nil)
	if err != nil {
		t.Fatalf("SpawnBackground: %v", err)
	}
	if id == "" {
		t.Fatal("no handle returned")
	}
	// It is tracked as running (or already done) the moment we hold the id.
	if _, ok := s.Background().Get(id); !ok {
		t.Fatalf("agent %q not registered", id)
	}

	waitFor(t, func() bool {
		a, _ := s.Background().Get(id)
		return a.Done()
	})
	a, _ := s.Background().Get(id)
	if a.Status != BackgroundSucceeded {
		t.Fatalf("status = %q, err = %q", a.Status, a.Err)
	}
	if a.Label != "read the notes" {
		t.Errorf("label = %q", a.Label)
	}
}

// A finished background agent's result is retrievable via the registry and,
// once collected, is not delivered a second time.
func TestBackgroundResultIsDeliveredOnce(t *testing.T) {
	dir, path := fixtureFile(t)
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu1", "Read", map[string]any{"file_path": path}),
	}}
	s := backgroundSpawner(t, provider, dir, &fakeWorktrees{})

	id, err := s.SpawnBackground("Explore", "read it", "task", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { a, _ := s.Background().Get(id); return a.Done() })

	report := s.Background().PendingReport()
	if !strings.Contains(report, id) || !strings.Contains(report, "succeeded") {
		t.Fatalf("first report missing the finished agent:\n%s", report)
	}
	if again := s.Background().PendingReport(); again != "" {
		t.Errorf("a collected result was delivered again:\n%s", again)
	}
}

// A read-only agent shares the parent tree; a writer is isolated in a worktree
// that is cleaned up when it finishes.
func TestWriterIsIsolatedAndCleanedUp(t *testing.T) {
	dir, path := fixtureFile(t)
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu1", "Read", map[string]any{"file_path": path}),
	}}
	wt := &fakeWorktrees{}
	s := backgroundSpawner(t, provider, dir, wt)

	// Explore is read-only: no worktree.
	roID, _ := s.SpawnBackground("Explore", "read", "ro", nil)
	waitFor(t, func() bool { a, _ := s.Background().Get(roID); return a.Done() })
	if got := wt.createdIDs(); len(got) != 0 {
		t.Errorf("a read-only agent was isolated: %v", got)
	}
	if a, _ := s.Background().Get(roID); a.Isolated {
		t.Error("read-only agent marked isolated")
	}

	// general-purpose has the wildcard toolset: a writer, so it is isolated.
	wID, _ := s.SpawnBackground("general-purpose", "write", "rw", nil)
	waitFor(t, func() bool { a, _ := s.Background().Get(wID); return a.Done() })
	if got := wt.createdIDs(); len(got) != 1 || got[0] != wID {
		t.Errorf("writer not isolated exactly once: %v", got)
	}
	waitFor(t, func() bool { return len(wt.cleanedIDs()) == 1 })
	if got := wt.cleanedIDs(); len(got) != 1 || got[0] != wID {
		t.Errorf("worktree not cleaned up: %v", got)
	}
	if a, _ := s.Background().Get(wID); !a.Isolated {
		t.Error("writer not marked isolated")
	}
}

// If the worktree cannot be created, the writer fails rather than silently
// running in the shared tree.
func TestWriterFailsWhenIsolationFails(t *testing.T) {
	dir, path := fixtureFile(t)
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu1", "Read", map[string]any{"file_path": path}),
	}}
	wt := &fakeWorktrees{failWith: errors.New("not a git repo")}
	s := backgroundSpawner(t, provider, dir, wt)

	id, _ := s.SpawnBackground("general-purpose", "write", "rw", nil)
	waitFor(t, func() bool { a, _ := s.Background().Get(id); return a.Done() })
	a, _ := s.Background().Get(id)
	if a.Status != BackgroundFailed {
		t.Fatalf("status = %q, want failed", a.Status)
	}
	if !strings.Contains(a.Err, "isolate") {
		t.Errorf("error does not explain the failure: %q", a.Err)
	}
}

// An unknown type is rejected before anything is registered.
func TestSpawnBackgroundRejectsUnknownType(t *testing.T) {
	s := backgroundSpawner(t, &scriptedProvider{}, t.TempDir(), &fakeWorktrees{})
	if _, err := s.SpawnBackground("bogus", "x", "l", nil); err == nil {
		t.Fatal("expected an unknown type to be rejected")
	}
	if got := s.Background().List(); len(got) != 0 {
		t.Errorf("a rejected launch still registered: %v", got)
	}
}

// The registry's List/status logic backs the /agents view: running agents show
// as running with a growing elapsed time, finished ones keep their result.
func TestRegistryListReportsStates(t *testing.T) {
	r := NewBackgroundRegistry()
	id1 := r.register("Explore", "search", false, nil)
	id2 := r.register("general-purpose", "build", true, nil)

	r.finish(id1, "found it", nil)
	r.finish(id2, "", fmt.Errorf("boom"))

	byID := map[string]BackgroundAgent{}
	for _, a := range r.List() {
		byID[a.ID] = a
	}
	if byID[id1].Status != BackgroundSucceeded || byID[id1].Result != "found it" {
		t.Errorf("id1 = %+v", byID[id1])
	}
	if byID[id2].Status != BackgroundFailed || byID[id2].Err != "boom" {
		t.Errorf("id2 = %+v", byID[id2])
	}
	if !byID[id2].Isolated {
		t.Error("writer should be marked isolated in the listing")
	}
	// A still-running agent reports a non-negative elapsed and Done()==false.
	id3 := r.register("Explore", "slow", false, nil)
	if a, _ := r.Get(id3); a.Done() || a.Elapsed() < 0 {
		t.Errorf("running agent: done=%v elapsed=%v", a.Done(), a.Elapsed())
	}
}

// Concurrent writers and readers of the registry must be race-free (run under
// -race). This exercises register/finish/setActivity/List/TakeFinished at once.
func TestBackgroundRegistryConcurrency(t *testing.T) {
	r := NewBackgroundRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := r.register("Explore", fmt.Sprintf("t%d", n), false, nil)
			r.setActivity(id, "Read x")
			r.finish(id, "done", nil)
		}(i)
	}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.List()
			_ = r.TakeFinished()
			_ = r.PendingReport()
		}()
	}
	wg.Wait()
	// Every registered agent eventually collected exactly once across polls.
	seen := map[string]bool{}
	for _, a := range r.List() {
		seen[a.ID] = true
	}
	if len(seen) != 20 {
		t.Errorf("registered %d agents, want 20", len(seen))
	}
}
