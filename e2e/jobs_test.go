package e2e

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A background job does not outlive the session: the binary tears its jobs
// down before it exits. This is the check that would have caught the
// orphaned-child bug smoke.sh describes, without a live model or a TCP port.
func TestBackgroundJobDiesWithSession(t *testing.T) {
	m := NewFakeModel(t,
		Use("Bash", map[string]any{"command": "echo $$ > job.pid; exec sleep 300", "run_in_background": true}),
		// Wait for the pid file so the check below is never vacuous.
		Use("Bash", map[string]any{"command": "while [ ! -s job.pid ]; do sleep 0.05; done; cat job.pid"}),
		Use("Jobs", map[string]any{}),
		Say("done."),
	)
	e := NewEnv(t, m)
	r := e.Headless("start a job", "--dangerously-skip-permissions")
	if r.ExitCode != 0 {
		t.Fatalf("exit %d\n%s", r.ExitCode, r.dump())
	}
	if got := strings.Join(toolUses(r.Events()), ","); got != "Bash,Bash,Jobs" {
		t.Errorf("tool uses = %s", got)
	}

	b, err := os.ReadFile(e.Path("job.pid"))
	if err != nil {
		t.Fatalf("job never started: %v\n%s", err, r.dump())
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatalf("pid file: %q", b)
	}

	// The session must have waited for teardown, so the process should already
	// be gone. The short grace only covers init reaping the zombie of a child
	// that was killed correctly.
	deadline := time.Now().Add(2 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("background job (pid %d) outlived the session", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// A command that would open an editor fails fast with actionable guidance
// instead of hanging on a missing terminal.
func TestEditorCommandFailsFast(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	m := NewFakeModel(t, Use("Bash", map[string]any{"command": "git commit"}), Say("done."))
	e := NewEnv(t, m)
	for _, args := range [][]string{
		{"init", "-q"},
		{"commit", "-q", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = e.Dir, e.environ()
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(e.Path("f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "f.txt")
	add.Dir, add.Env = e.Dir, e.environ()
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	r := e.Headless("commit", "--dangerously-skip-permissions")
	if r.Elapsed > 30*time.Second {
		t.Errorf("git commit took %s; an editor-opening command should fail fast", r.Elapsed)
	}
	results := toolResults(r.Events())
	if len(results) != 1 {
		t.Fatalf("want one tool result\n%s", r.dump())
	}
	if !strings.Contains(resultText(results[0]), "-m") {
		t.Errorf("the failure does not name -m: %q", resultText(results[0]))
	}
}
