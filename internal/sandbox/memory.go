package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// A memory limit caps what one Bash command — the whole process tree it starts,
// not just the shell — may hold in RAM. A runaway test or build then dies alone
// (the kernel's OOM killer picks inside the command's cgroup) instead of pushing
// the machine into swap and taking the editor, the browser and Klaudia with it.
//
// On Linux the limit is a cgroup v2 memory controller, and an unprivileged
// process cannot make one of those on its own: the cgroup it runs in already
// holds processes, and cgroup v2 only enables a controller for children of a
// cgroup that holds none. The component that owns the user's cgroup subtree —
// the systemd user manager, under the delegation every current systemd distro
// grants user@.service — can, and `systemd-run --user --scope` asks it to. The
// scope is created, systemd-run moves itself into it and execs the command, so
// the command keeps its pid, its process group and its environment; only its
// cgroup changes.
//
// A container carries its own cgroup, so there the limit is `--memory`.

// ParseMemory reads a memory size: a whole number of bytes with an optional
// K, M, G or T suffix (powers of 1024, case-insensitive, an optional trailing
// "B" or "iB" accepted). "2G", "512m" and "1536MiB" are all valid.
func ParseMemory(s string) (int64, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, fmt.Errorf("memory size is empty")
	}
	u := strings.ToUpper(t)
	if strings.HasSuffix(u, "IB") {
		u = u[:len(u)-2]
	} else if strings.HasSuffix(u, "B") {
		u = u[:len(u)-1]
	}
	mult := int64(1)
	if u != "" {
		switch u[len(u)-1] {
		case 'K':
			mult, u = 1<<10, u[:len(u)-1]
		case 'M':
			mult, u = 1<<20, u[:len(u)-1]
		case 'G':
			mult, u = 1<<30, u[:len(u)-1]
		case 'T':
			mult, u = 1<<40, u[:len(u)-1]
		}
	}
	n, err := strconv.ParseInt(u, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("memory size %q is not a positive number with an optional K/M/G/T suffix", s)
	}
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("memory size %q is too large", s)
	}
	return n * mult, nil
}

// MemoryScope runs its inner executor's commands inside a transient systemd
// scope whose cgroup has MemoryMax set, so the command and everything it starts
// share one memory budget. Swap is capped at zero: otherwise a command over its
// limit pages out instead of being stopped, which is the slowdown the limit is
// there to prevent.
type MemoryScope struct {
	Inner Executor
	Bytes int64
}

// NewMemoryScope wraps inner so every command it runs is limited to bytes.
func NewMemoryScope(inner Executor, bytes int64) *MemoryScope {
	return &MemoryScope{Inner: inner, Bytes: bytes}
}

func (m *MemoryScope) Name() string { return m.Inner.Name() + "+memory" }

func (m *MemoryScope) Argv(req Request) (string, []string) {
	name, args := m.Inner.Argv(req)
	return "systemd-run", append(memoryScopeArgs(m.Bytes), append([]string{name}, args...)...)
}

func (m *MemoryScope) Run(ctx context.Context, req Request) (Response, error) {
	name, args := m.Argv(req)
	return runArgv(ctx, req, name, args)
}

// memoryScopeArgs are the systemd-run arguments up to and including the "--"
// that separates them from the command. --collect removes the scope when the
// command exits even if it failed, so failed commands do not accumulate units.
func memoryScopeArgs(bytes int64) []string {
	b := strconv.FormatInt(bytes, 10)
	return []string{"--user", "--scope", "--quiet", "--collect",
		"-p", "MemoryMax=" + b, "-p", "MemorySwapMax=0", "--"}
}

// ProbeMemoryScope checks that a memory limit set through systemd-run is
// actually enforced here, and says why not when it is not.
//
// Starting a scope is not enough to know that. Where the memory controller is
// not delegated to the user manager, systemd accepts MemoryMax and silently
// enforces nothing; where there is no user session bus (a container, a cron
// job, an ssh login without lingering) systemd-run fails outright. So the probe
// runs a command in a limited scope that reads its own cgroup's memory.max,
// and only a number there counts.
func ProbeMemoryScope(ctx context.Context, bytes int64) error {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return fmt.Errorf("systemd-run not found")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := append(memoryScopeArgs(bytes), "/bin/sh", "-c",
		`cat "/sys/fs/cgroup$(sed -n 's/^0:://p' /proc/self/cgroup)/memory.max"`)
	out, err := exec.CommandContext(ctx, "systemd-run", args...).CombinedOutput()
	got := strings.TrimSpace(string(out))
	if err != nil {
		if got == "" {
			got = err.Error()
		}
		return fmt.Errorf("systemd-run could not start a limited scope (%s)", firstLine(got))
	}
	if n, perr := strconv.ParseInt(got, 10, 64); perr != nil || n <= 0 || n > bytes {
		return fmt.Errorf("the memory controller is not enforced for this user (the scope's memory.max reads %q)", firstLine(got))
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
