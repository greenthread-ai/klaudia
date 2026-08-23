//go:build unix

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Every command runs in its own process group, and cancelling one signals the
// whole group.
//
// Without this, `exec.CommandContext` signals only the process it started —
// which is the shell, not the thing the shell started. Cancelling
// `sh -c "npm run dev"` reaps the shell and leaves node holding port 3000, so
// stopping a job does not stop it, Esc during a command does not end it, and
// the next `npm run dev` becomes a second copy fighting the first for the port.
// Verified before the change: a `sleep 300` grandchild survived cancel + Wait.
//
// Klaudia also stops being in the same group as its children, which means a
// Ctrl+C at the terminal no longer reaches them by accident. That is the point
// — a stray signal should not kill a managed job — but it makes the CLI's own
// signal handling load-bearing, so cmd/klaudia installs one and cancels the
// root context on SIGINT/SIGTERM.

// killGrace is how long the group gets to exit after SIGTERM before SIGKILL.
// Long enough for a dev server to close its listener and flush, short enough
// that stopping a job feels immediate.
const killGrace = 2 * time.Second

// applyProcGroup puts cmd in a new process group and routes cancellation
// through the group rather than the single child.
func applyProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return terminateGroup(cmd.Process) }
}

// terminateGroup sends SIGTERM to the process's group, then SIGKILL after
// killGrace if anything is still alive.
//
// SIGTERM first because a dev server that gets it releases its port and flushes
// its log; SIGKILL alone leaves a socket in TIME_WAIT and truncates the last
// few lines, which are usually the ones explaining why it is being stopped.
func terminateGroup(p *os.Process) error {
	if p == nil {
		return os.ErrProcessDone
	}
	pgid, err := syscall.Getpgid(p.Pid)
	if err != nil {
		// The child is already gone, or was never placed in its own group.
		// Fall back to the single process rather than signalling pgid 0, which
		// would mean "this process's group" — i.e. Klaudia itself.
		return p.Kill()
	}
	if pgid == syscall.Getpgrp() {
		// Setpgid did not take effect. Signalling -pgid here would kill
		// Klaudia. Refuse and take the narrow path.
		return p.Kill()
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	go func() {
		time.Sleep(killGrace)
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	}()
	return nil
}
