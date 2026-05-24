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
