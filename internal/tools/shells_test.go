package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// waitUntil polls fn until it returns true or the deadline passes.
func waitUntil(d time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fn()
}

func TestShellStoreRunsAndReportsExit(t *testing.T) {
	store := NewShellStore(context.Background())
	id, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "printf 'hello\\nworld\\n'"})
	if err != nil {
		t.Fatal(err)
	}

	var final ShellOutput
	if !waitUntil(3*time.Second, func() bool {
		out, ok := store.Read(id)
		if !ok {
			return false
		}
		// Read advances the offset, so accumulate across polls.
		final.Output += out.Output
		final.Running, final.ExitCode = out.Running, out.ExitCode
		return !out.Running
	}) {
		t.Fatal("shell did not exit in time")
	}
	if !strings.Contains(final.Output, "hello") || !strings.Contains(final.Output, "world") {
		t.Errorf("output = %q", final.Output)
	}
	if final.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", final.ExitCode)
	}
}

func TestShellStoreKillStopsLongRunning(t *testing.T) {
	store := NewShellStore(context.Background())
	id, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	out, ok := store.Read(id)
	if !ok || !out.Running {
		t.Fatalf("expected a running shell, got ok=%v running=%v", ok, out.Running)
	}
	if !store.Kill(id) {
		t.Fatal("Kill returned false")
	}
	if !waitUntil(3*time.Second, func() bool {
		o, _ := store.Read(id)
		return !o.Running
	}) {
		t.Error("killed shell still running")
	}
}

func TestShellStoreUnknownID(t *testing.T) {
	store := NewShellStore(context.Background())
	if _, ok := store.Read("bash_999"); ok {
		t.Error("Read of unknown id should report not-ok")
	}
	if store.Kill("bash_999") {
		t.Error("Kill of unknown id should report false")
	}
}

func TestBashOutputToolFilter(t *testing.T) {
	store := NewShellStore(context.Background())
	id, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "printf 'keep me\\ndrop this\\nkeep too\\n'"})
	// Wait for exit via the non-consuming List (so the tool reads from offset 0).
	waitUntil(3*time.Second, func() bool { return !peekRunning(store, id) })

	tool, _ := NewBashOutput(store)
	res, err := tool.Execute(context.Background(), Context{}, []byte(`{"bash_id":"`+id+`","filter":"keep"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := res[0].Content
	if !strings.Contains(got, "keep me") || !strings.Contains(got, "keep too") || strings.Contains(got, "drop this") {
		t.Errorf("filtered output = %q", got)
	}
	if !strings.Contains(got, "shell exited") {
		t.Errorf("expected exit annotation, got %q", got)
	}
}

// peekRunning reports a shell's running state without consuming its output.
func peekRunning(s *ShellStore, id string) bool {
	for _, sh := range s.List() {
		if sh.ID == id {
			return sh.Running
		}
	}
	return false
}
