package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// Container runs commands inside a container via the docker/podman CLI. It is
// the executor for the "run code in a pre-built container to validate work"
// flow: the model writes files to the host working directory (mounted in), then
// executes/tests them in an isolated, reproducible environment.
//
// Shelling to the CLI (vs. linking the Docker SDK) keeps the binary lean and
// CGO-free and works with Docker or Podman interchangeably.
type Container struct {
	Runtime  string // "docker" or "podman"
	Image    string // image to run, e.g. "python:3.12-slim"
	MountCWD bool   // bind-mount the working directory at the same path
	ReadOnly bool   // mount the working directory read-only
	Network  string // docker --network value; "none" isolates fully ("" = default)
}

// NewContainer builds a container executor. runtime defaults to "docker".
func NewContainer(runtime, image string, mountCWD, readOnly bool, network string) *Container {
	if runtime == "" {
		runtime = "docker"
	}
	return &Container{Runtime: runtime, Image: image, MountCWD: mountCWD, ReadOnly: readOnly, Network: network}
}

func (c *Container) Name() string { return c.Runtime + ":" + c.Image }

// RuntimeAvailable reports whether the named container runtime is on PATH.
func RuntimeAvailable(runtime string) bool {
	if runtime == "" {
		runtime = "docker"
	}
	_, err := exec.LookPath(runtime)
	return err == nil
}

// buildArgs assembles the `docker run` arguments for a request. Separated from
// Run so it can be unit-tested without a daemon.
func (c *Container) buildArgs(req Request) []string {
	args := []string{"run", "--rm", "-i"}
	if c.Network != "" {
		args = append(args, "--network", c.Network)
	}
	if c.MountCWD && req.WorkingDir != "" {
		mount := req.WorkingDir + ":" + req.WorkingDir
		if c.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount, "-w", req.WorkingDir)
	}
	for _, e := range req.Env {
		args = append(args, "-e", e)
	}
	args = append(args, c.Image, "sh", "-c", req.Command)
	return args
}

func (c *Container) Argv(req Request) (string, []string) {
	return c.Runtime, c.buildArgs(req)
}

// Run executes the command inside a fresh container.
func (c *Container) Run(ctx context.Context, req Request) (Response, error) {
	if c.Image == "" {
		return Response{}, fmt.Errorf("container executor: no image configured")
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, c.Runtime, c.buildArgs(req)...)
	cmd.WaitDelay = postCancelWait // bound post-cancel I/O drain — see runArgv comment
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	resp := Response{Stdout: stdout.String(), Stderr: stderr.String()}
	if ctx.Err() == context.DeadlineExceeded {
		resp.TimedOut = true
		resp.ExitCode = 124
		return resp, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			resp.ExitCode = exitErr.ExitCode()
			return resp, nil
		}
		// docker itself failed to run (daemon down, image missing, etc.).
		return resp, fmt.Errorf("%s run failed: %w (%s)", c.Runtime, err, stderr.String())
	}
	return resp, nil
}
