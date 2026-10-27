//go:build darwin || linux

package e2e

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Keys that are not text. Type sends text; Press sends these.
const (
	keyEnter = "\r"
	keyEsc   = "\x1b"
	keyCtrlC = "\x03"
	keyDown  = "\x1b[B"
)

// Term is the klaudia binary running in a pseudo-terminal, with its output
// fed through a Screen.
type Term struct {
	t   *testing.T
	pty *os.File
	cmd *exec.Cmd

	mu     sync.Mutex
	screen *Screen

	exited  chan struct{}
	exitErr error
}

type winsize struct{ rows, cols, x, y uint16 }

// startTerm runs bin with args in dir, in a cols×rows pty, with exactly env.
func startTerm(t *testing.T, bin, dir string, env, args []string, cols, rows int) *Term {
	t.Helper()
	master, slaveName, err := openPTY()
	if err != nil {
		t.Fatalf("pty: %v", err)
	}
	slave, err := os.OpenFile(slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", slaveName, err)
	}
	// Set on the slave: macOS refuses TIOCSWINSZ on the master.
	ws := winsize{rows: uint16(rows), cols: uint16(cols)}
	if err := ioctl(int(slave.Fd()), syscall.TIOCSWINSZ, unsafe.Pointer(&ws)); err != nil {
		t.Fatalf("set window size: %v", err)
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// A new session with the pty as its controlling terminal, as a shell
	// would start it; Ctty is the child's fd 0.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", bin, err)
	}
	slave.Close() // the child has its own; ours would keep reads from ever seeing EOF

	tm := &Term{t: t, pty: master, cmd: cmd, screen: newScreen(cols, rows), exited: make(chan struct{})}
	go tm.read()
	go func() {
		tm.exitErr = cmd.Wait()
		close(tm.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-tm.exited:
		default:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // the whole session: Bash children too
			<-tm.exited
		}
		master.Close()
		if t.Failed() {
			t.Logf("final screen:\n%s", tm.Snapshot())
		}
	})
	return tm
}

// read feeds output to the screen and writes back the answers to any
// terminal queries in it, in order.
func (tm *Term) read() {
	buf := make([]byte, 32*1024)
	for {
		n, err := tm.pty.Read(buf)
		if n > 0 {
			tm.mu.Lock()
			tm.screen.Feed(buf[:n])
			replies := tm.screen.takeReplies()
			tm.mu.Unlock()
			if len(replies) > 0 {
				_, _ = tm.pty.Write(replies)
			}
		}
		if err != nil {
			return // EIO once the child and every holder of the slave are gone
		}
	}
}

// Snapshot is the terminal as it is now.
func (tm *Term) Snapshot() Snapshot {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.screen.Snapshot()
}

// Type sends text one key at a time, as a person would; a burst of runes
// can reach the program as a single paste-like message.
func (tm *Term) Type(s string) {
	for _, r := range s {
		tm.Press(string(r))
		time.Sleep(5 * time.Millisecond)
	}
}

// Press sends raw bytes: a key constant, or any escape sequence.
func (tm *Term) Press(keys string) {
	if _, err := tm.pty.Write([]byte(keys)); err != nil {
		tm.t.Fatalf("write to pty: %v", err)
	}
}

// WaitFor polls the screen until cond holds, and fails the test with the
// screen dumped if it does not within timeout.
func (tm *Term) WaitFor(t *testing.T, what string, timeout time.Duration, cond func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		snap := tm.Snapshot()
		if cond(snap) {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s\n%s", timeout, what, snap)
		}
		select {
		case <-tm.exited:
			// One last look: the exit may have flushed the final frame.
			if snap := tm.Snapshot(); cond(snap) {
				return snap
			}
			t.Fatalf("klaudia exited (%v) while waiting for %s\n%s", tm.exitErr, what, tm.Snapshot())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// WaitText waits for substr anywhere in scrollback or on screen.
func (tm *Term) WaitText(t *testing.T, substr string, timeout time.Duration) Snapshot {
	t.Helper()
	return tm.WaitFor(t, strings.TrimSpace(substr), timeout, func(s Snapshot) bool {
		return strings.Contains(s.All(), substr)
	})
}

// WaitExit waits up to timeout for the process to end, and reports whether
// it did and with what error.
func (tm *Term) WaitExit(timeout time.Duration) (exited bool, err error) {
	select {
	case <-tm.exited:
		return true, tm.exitErr
	case <-time.After(timeout):
		return false, nil
	}
}
