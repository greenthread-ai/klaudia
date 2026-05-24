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
	"time"
)

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

func (l *Local) Run(ctx context.Context, req Request) (Response, error) {
	return runArgv(ctx, req, shellPath, []string{"-c", req.Command})
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
