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
	"os"
	"os/exec"
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
	Env        []string      // extra environment; empty means inherit os.Environ
}

// Response is the result of executing a Request.
type Response struct {
	Stdout   string
	Stderr   string
	ExitCode int
	TimedOut bool
}

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
	if len(req.Env) > 0 {
		cmd.Env = append(os.Environ(), req.Env...)
	}
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
	done     bool
	exitCode int
	runErr   error
	cancel   context.CancelFunc
}

// Write appends process output to the buffer (cmd.Stdout/Stderr both target it,
// so output is combined in arrival order).
func (p *BackgroundProcess) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	return len(b), nil
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
	ctx, cancel := context.WithCancel(parent)
	name, args := e.Argv(req)
	cmd := exec.CommandContext(ctx, name, args...)
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}
	if len(req.Env) > 0 {
		cmd.Env = append(os.Environ(), req.Env...)
	}
	cmd.WaitDelay = postCancelWait // same I/O drain guard as runArgv
	p := &BackgroundProcess{cancel: cancel}
	cmd.Stdout = p
	cmd.Stderr = p // same writer ⇒ exec serializes both streams into one pipe
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		p.finish(err)
		cancel()
	}()
	return p, nil
}
