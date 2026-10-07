package sandbox

import (
	"slices"
	"strings"
	"testing"
)

func TestContainerBuildArgs(t *testing.T) {
	c := NewContainer("docker", "python:3.12-slim", true, false, "none")
	args := c.buildArgs(Request{Command: "python hi.py", WorkingDir: "/work/proj"})

	joined := strings.Join(args, " ")
	for _, want := range []string{
		"run --rm -i",
		"--network none",
		"-v /work/proj:/work/proj",
		"-w /work/proj",
		"python:3.12-slim sh -c python hi.py",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q\n got: %s", want, joined)
		}
	}
}

func TestContainerReadOnlyMount(t *testing.T) {
	c := NewContainer("docker", "alpine", true, true, "")
	args := c.buildArgs(Request{Command: "ls", WorkingDir: "/w"})
	if !slices.Contains(args, "/w:/w:ro") {
		t.Errorf("expected read-only mount, got %v", args)
	}
}

func TestContainerNoMountWhenDisabled(t *testing.T) {
	c := NewContainer("podman", "alpine", false, false, "")
	args := c.buildArgs(Request{Command: "echo hi", WorkingDir: "/w"})
	if slices.Contains(args, "-v") {
		t.Errorf("did not expect a -v mount, got %v", args)
	}
	if c.Name() != "podman:alpine" {
		t.Errorf("Name = %q", c.Name())
	}
}

func TestContainerEnvPassthrough(t *testing.T) {
	c := NewContainer("docker", "alpine", false, false, "")
	args := c.buildArgs(Request{Command: "env", Env: []string{"FOO=bar"}})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-e FOO=bar") {
		t.Errorf("expected env passthrough, got %s", joined)
	}
}

func TestContainerMemoryLimit(t *testing.T) {
	c := NewContainer("docker", "alpine", true, false, "")
	if args := c.buildArgs(Request{Command: "ls"}); slices.Contains(args, "--memory") {
		t.Errorf("no limit configured, but args carry --memory: %q", args)
	}
	c.Memory = 512 << 20
	joined := strings.Join(c.buildArgs(Request{Command: "ls"}), " ")
	if !strings.Contains(joined, "--memory 536870912 --memory-swap 536870912") {
		t.Errorf("args missing the memory limit: %s", joined)
	}
	if strings.Index(joined, "--memory") > strings.Index(joined, "alpine") {
		t.Errorf("--memory must come before the image: %s", joined)
	}
}
