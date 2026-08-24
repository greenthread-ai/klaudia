// Package sandbox runs shell commands for the Bash tool behind an Executor
// interface, so the execution strategy (plain host process, OS sandbox via
// sandbox-exec/bwrap, or a pre-built container) can be swapped without touching
// the tool.
//
// Phase 2 ships the Local executor (plain host process with timeout + output
// capture). The OS-confinement executors (seatbelt.go / bwrap.go) and the
// container executor land in Phase 6; this interface is their seam.
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// postCancelWait is the budget WaitDelay gets to drain stdout/stderr pipes
// after the parent process exits or ctx is cancelled. Picked so a well-behaved
// command can flush a few lines of output (most do this in milliseconds) but
// an inherited-fd-leak from a backgrounded child (npm run dev, dev servers,
// long-lived daemons) can't pin the goroutine forever. The TUI surfaces this
// as a bounded TimedOut + ExitCode 124 instead of a 43-minute spinner.
const postCancelWait = 5 * time.Second

// Request describes a command to execute.
type Request struct {
	Command    string        // the shell command line
	WorkingDir string        // cwd; empty means inherit
	Timeout    time.Duration // 0 means no explicit timeout
	Env        []string      // extra environment, layered over the user's (see env.go)
}

// Response is the result of executing a Request.
type Response struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

// TTYRequired reports whether a command needs a terminal this executor cannot
// provide, along with the non-interactive alternative to suggest. Exported so
// the Bash tool can refuse before launching rather than hang until the timeout.
func TTYRequired(command string) (reason string, blocked bool) { return ttyRequired(command) }

// Executor runs a command and returns its captured output.
type Executor interface {
	// Name identifies the executor (e.g. "local", "sandbox-exec", "docker").
	Name() string
	// Run executes req. A non-nil error indicates a failure to run the command
	// at all (vs. the command running and exiting non-zero, which is reported
	// via Response.ExitCode).
	Run(ctx context.Context, req Request) (Response, error)
	// Argv returns the program and arguments this executor would run for req
	// (e.g. the bare shell locally, or the sandbox-exec/bwrap/docker wrapper).
	// It is the single source of truth shared by Run and StartBackground.
	Argv(req Request) (name string, args []string)
}

// shellPath is the shell used to interpret command lines. bash if available,
// else sh. Resolved once.
var shellPath = resolveShell()

func resolveShell() string {
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "/bin/sh"
}

// Local runs commands as plain host processes with no confinement. It is the
// default executor and the fallback when no sandbox/container is available.
type Local struct{}

// NewLocal returns the unconfined host executor.
func NewLocal() *Local { return &Local{} }

func (l *Local) Name() string { return "local" }

func (l *Local) Argv(req Request) (string, []string) {
	return shellPath, []string{"-c", req.Command}
}

func (l *Local) Run(ctx context.Context, req Request) (Response, error) {
	name, args := l.Argv(req)
	return runArgv(ctx, req, name, args)
}

func runArgv(ctx context.Context, req Request, name string, args []string) (Response, error) {
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, name, args...)
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	cmd.Env = childEnv(req)
	// Own process group, cancelled as a group. See procgroup_unix.go: without
	// it, cancelling `sh -c "…"` reaps the shell and leaves whatever it started
	// running.
	applyProcGroup(cmd)
	// Bound how long cmd.Run keeps waiting for stdout/stderr pipes after the
	// parent process exits or ctx is cancelled. Without this, a command like
	// `npm run dev & sleep 1` returns instantly from bash (because sleep 1
	// finished) but Go keeps waiting forever for the backgrounded npm
	// process's pipes — npm inherited bash's stdout/stderr fds and never
	// closes them. WaitDelay sends SIGKILL to lingering children and bounds
	// the I/O copy at this duration. The result might be marked TimedOut
	// (ExitCode 124), which is more informative than "still working… 43m".
	cmd.WaitDelay = postCancelWait

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	resp := Response{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if ctx.Err() == context.DeadlineExceeded {
		resp.TimedOut = true
		resp.ExitCode = 124 // conventional timeout exit code
		return resp, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			resp.ExitCode = exitErr.ExitCode()
			return resp, nil
		}
		// Couldn't start the command at all.
		return resp, err
	}
	return resp, nil
}

// BackgroundProcess is a detached command whose combined stdout+stderr is
// buffered and can be read incrementally while it runs. Safe for concurrent use.
type BackgroundProcess struct {
	mu       sync.Mutex
	buf      []byte
	sink     io.Writer // when set, output goes here instead of buf
	done     bool
	exitCode int
	runErr   error
	cancel   context.CancelFunc
	pid      int
}

// BackgroundOptions configures a detached launch.
type BackgroundOptions struct {
	// Sink receives the combined output instead of an in-memory buffer. A
	// long-lived dev server produces more output than a process should hold in
	// memory, and a log that only exists in RAM cannot be paged or searched.
	Sink io.Writer
	// OnExit is called once when the process finishes, however it finished.
	// Without it, nothing learns that a job died until something reads it —
	// so a crashed dev server stays "running" until the model happens to poll.
	OnExit func(exitCode int)
}

// Write appends process output to the buffer (cmd.Stdout/Stderr both target it,
// so output is combined in arrival order).
func (p *BackgroundProcess) Write(b []byte) (int, error) {
	p.mu.Lock()
	sink := p.sink
	if sink == nil {
		p.buf = append(p.buf, b...)
	}
	p.mu.Unlock()
	if sink != nil {
		return sink.Write(b)
	}
	return len(b), nil
}

// Pid is the process id of the launched command, or 0 once it has exited.
// Used to prove a restart actually replaced the process rather than adopting
// the old one.
func (p *BackgroundProcess) Pid() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// Read returns buffered output from offset onward, the new offset, whether the
// process has exited, and (once exited) its exit code.
func (p *BackgroundProcess) Read(offset int) (data string, newOffset int, done bool, exitCode int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if offset < 0 || offset > len(p.buf) {
		offset = len(p.buf)
	}
	return string(p.buf[offset:]), len(p.buf), p.done, p.exitCode
}

// WaitExit blocks until the process has exited or d elapses, reporting whether
// it exited. Used at session teardown, where returning before the process is
// actually gone would abandon it: the SIGTERM→SIGKILL escalation runs in a
// goroutine, and a goroutine does not outlive os.Exit.
func (p *BackgroundProcess) WaitExit(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !p.Running() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !p.Running()
}

// KillGrace is how long a process group gets between SIGTERM and SIGKILL.
// Exported so callers that must wait for teardown know how long that is.
func KillGrace() time.Duration { return killGrace }

// Running reports whether the process is still executing.
func (p *BackgroundProcess) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.done
}

// Kill terminates the process (no-op if already exited).
func (p *BackgroundProcess) Kill() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *BackgroundProcess) finish(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done = true
	p.pid = 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			p.exitCode = exitErr.ExitCode()
		} else {
			p.exitCode = -1
			p.runErr = err
		}
	}
}

// StartBackground launches req detached using e's argv, streaming combined
// output into the returned process. The process is killed if parent is
// cancelled or BackgroundProcess.Kill is called. A non-nil error means the
// command could not be started.
func StartBackground(parent context.Context, e Executor, req Request) (*BackgroundProcess, error) {
	return StartBackgroundWith(parent, e, req, BackgroundOptions{})
}

// StartBackgroundWith is StartBackground with an output sink and an exit
// callback.
func StartBackgroundWith(parent context.Context, e Executor, req Request, opts BackgroundOptions) (*BackgroundProcess, error) {
	ctx, cancel := context.WithCancel(parent)
	name, args := e.Argv(req)
	cmd := exec.CommandContext(ctx, name, args...)
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	cmd.Env = childEnv(req)
	applyProcGroup(cmd)
	// WaitDelay bounds the I/O drain, but it must outlast the group's
	// SIGTERM→SIGKILL escalation: expiring first would abandon the pipes while
	// the process is still being asked to leave, losing the last lines of a log
	// exactly when they matter.
	cmd.WaitDelay = postCancelWait + killGrace
	p := &BackgroundProcess{cancel: cancel, sink: opts.Sink}
	cmd.Stdout = p
	cmd.Stderr = p // same writer ⇒ exec serializes both streams into one pipe
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	p.mu.Lock()
	p.pid = cmd.Process.Pid
	p.mu.Unlock()
	go func() {
		err := cmd.Wait()
		p.finish(err)
		cancel()
		if opts.OnExit != nil {
			opts.OnExit(p.ExitCode())
		}
	}()
	return p, nil
}

// ExitCode reports the process's exit status once it has finished.
func (p *BackgroundProcess) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitCode
}

// resolveRoots canonicalises write roots for a sandbox profile.
//
// Both seatbelt and bubblewrap match on the real path, so an unresolved
// symlink silently denies writes to the very directory being allowed. On macOS
// this is not an edge case: /tmp is a symlink to /private/tmp and /var to
// /private/var, so a project under either — or any user whose home or checkout
// sits behind a symlink — would find the sandbox refusing writes to its own
// project. Verified: an unresolved /tmp root produces "Operation not permitted"
// on a write inside it, while the resolved form succeeds.
//
// Paths that don't resolve (not yet created, permission denied on a parent) are
// kept as given: an allow-rule for a path that doesn't exist is harmless, and
// dropping it would silently narrow the sandbox instead.
func resolveRoots(roots []string) []string {
	out := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, r := range roots {
		if r == "" {
			continue
		}
		resolved := r
		if abs, err := filepath.Abs(r); err == nil {
			resolved = abs
		}
		if real, err := filepath.EvalSymlinks(resolved); err == nil {
			resolved = real
		}
		if !seen[resolved] {
			seen[resolved] = true
			out = append(out, resolved)
		}
	}
	return out
}
