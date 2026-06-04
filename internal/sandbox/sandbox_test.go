package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLocalRunCapturesOutputAndExit(t *testing.T) {
	l := NewLocal()
	resp, err := l.Run(context.Background(), Request{Command: "echo hello; echo oops >&2; exit 3"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(resp.Stdout, "hello") {
		t.Errorf("stdout = %q, want hello", resp.Stdout)
	}
	if !strings.Contains(resp.Stderr, "oops") {
		t.Errorf("stderr = %q, want oops", resp.Stderr)
	}
	if resp.ExitCode != 3 {
		t.Errorf("exit = %d, want 3", resp.ExitCode)
	}
}

func TestLocalRunTimeout(t *testing.T) {
	l := NewLocal()
	resp, err := l.Run(context.Background(), Request{Command: "sleep 5", Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !resp.TimedOut {
		t.Errorf("expected TimedOut, got %+v", resp)
	}
}

func TestLocalRunDoesNotHangOnLeakedChildPipes(t *testing.T) {
	// Repro of the real-world hang: a tool call like `npm run dev & sleep 1`
	// backgrounds a long-lived child that inherits bash's stdout/stderr fds.
	// The bash exits immediately (sleep 1 finishes), but cmd.Run would wait
	// forever for the child's pipes to close — they never do, because the
	// child keeps running. WaitDelay must bound the wait. We emulate it with
	// `sh -c "(sleep 30 &) ; exit 0"` — the subshell backgrounds a 30s sleep
	// that inherits stdout/stderr; without WaitDelay, Run blocks for ~30s.
	// With WaitDelay = 5s, the call returns in well under 10s.
	l := NewLocal()
	deadline := time.Now().Add(15 * time.Second)
	done := make(chan struct{})
	go func() {
		_, _ = l.Run(context.Background(), Request{Command: "(sleep 30 &) ; exit 0"})
		close(done)
	}()
	select {
	case <-done:
		if remaining := time.Until(deadline); remaining < 5*time.Second {
			t.Errorf("Run returned but took close to or past the 15s deadline (remaining=%s) — WaitDelay may not be firing", remaining)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run hung past 15s with a backgrounded child holding the pipes — WaitDelay missing")
	}
}

func TestLocalRunWorkingDir(t *testing.T) {
	dir := t.TempDir()
	l := NewLocal()
	resp, _ := l.Run(context.Background(), Request{Command: "pwd", WorkingDir: dir})
	// macOS may resolve /var -> /private/var symlinks; compare suffix.
	if !strings.Contains(resp.Stdout, strings.TrimPrefix(dir, "/private")) &&
		!strings.Contains(resp.Stdout, dir) {
		t.Errorf("pwd = %q, want to contain %q", strings.TrimSpace(resp.Stdout), dir)
	}
}
