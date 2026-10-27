package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBackgroundRunsAndReports(t *testing.T) {
	done := make(chan BackgroundTask, 2)
	b := NewBackground(context.Background(), func(t BackgroundTask) { done <- t })
	release := make(chan struct{})
	id := b.Start("Explore: survey", func(ctx context.Context, progress func(string)) (string, error) {
		progress("Read go.mod")
		<-release
		return "found three things", nil
	})
	if id != "agent_1" {
		t.Fatalf("id = %q", id)
	}
	deadline := time.Now().Add(2 * time.Second)
	for b.List()[0].Last != "Read go.mod" {
		if time.Now().After(deadline) {
			t.Fatal("progress line never recorded")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if b.Running() != 1 {
		t.Fatalf("Running = %d while working", b.Running())
	}
	close(release)
	got := <-done
	if got.Running || got.Result != "found three things" || got.Err != nil {
		t.Fatalf("finished task = %+v", got)
	}
	if n := got.Notice(); !strings.Contains(n, `status="completed"`) || !strings.Contains(n, "found three things") || !strings.Contains(n, "Explore: survey") {
		t.Errorf("notice = %q", n)
	}
}

// Stop cancels the task's context; the owner still hears about it, as stopped.
func TestBackgroundStop(t *testing.T) {
	done := make(chan BackgroundTask, 1)
	b := NewBackground(context.Background(), func(t BackgroundTask) { done <- t })
	id := b.Start("general-purpose: long job", func(ctx context.Context, _ func(string)) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !b.Stop(id) || b.Stop("agent_99") {
		t.Fatal("Stop should succeed for a running task and fail for an unknown one")
	}
	got := <-done
	if !errors.Is(got.Err, context.Canceled) || !strings.Contains(got.Notice(), `status="stopped"`) {
		t.Errorf("stopped task = %+v, notice %q", got, got.Notice())
	}
}

// The runner's lifetime is its owner's context, not a turn's.
func TestBackgroundEndsWithItsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan BackgroundTask, 1)
	b := NewBackground(ctx, func(t BackgroundTask) { done <- t })
	b.Start("x", func(ctx context.Context, _ func(string)) (string, error) { <-ctx.Done(); return "", ctx.Err() })
	cancel()
	select {
	case got := <-done:
		if got.Err == nil {
			t.Error("want an error when the owner's context ends")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task outlived its owner's context")
	}
}
