//go:build unix

package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The bug this fixes: cancelling `sh -c "…"` reaped the shell and left whatever
// it started running. A dev server kept its port, the next start became a
// second copy, and "stop the job" did not stop the job.
//
// The marker is a unique string in the grandchild's argv so the check cannot
// match some unrelated sleep on the developer's machine.
func TestKillReachesGrandchildren(t *testing.T) {
	marker := "klaudia-orphan-probe-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	p, err := StartBackground(context.Background(), NewLocal(), Request{
		// The shell backgrounds a child and waits, exactly like a dev server
		// launched through a wrapper script.
		Command: fmt.Sprintf("sh -c 'sleep 60 # %s' & echo started; wait", marker),
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		out, _, _, _ := p.Read(0)
		return strings.Contains(out, "started")
	}, "grandchild never started")

	if n := countProcesses(t, marker); n == 0 {
		t.Fatal("grandchild is not running, so the test proves nothing")
	}
	p.Kill()

	waitFor(t, func() bool { return countProcesses(t, marker) == 0 },
		"the grandchild survived Kill — the process group was not signalled")
}

// A foreground command's descendants have to go too: Esc during `npm test`
// should not leave the test runner's workers behind.
func TestForegroundCancelReachesGrandchildren(t *testing.T) {
	marker := "klaudia-fg-probe-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = NewLocal().Run(ctx, Request{
			Command: fmt.Sprintf("sh -c 'sleep 60 # %s' & wait", marker),
		})
	}()
	waitFor(t, func() bool { return countProcesses(t, marker) > 0 }, "grandchild never started")

	cancel()
	<-done
	waitFor(t, func() bool { return countProcesses(t, marker) == 0 },
		"the grandchild survived cancellation")
}

// Klaudia must never signal its own group. If Setpgid somehow did not take,
// -pgid would be Klaudia's own process group and terminateGroup would kill the
// session it is trying to protect.
func TestTerminateGroupNeverSignalsOurOwnGroup(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 5")
	// Deliberately NOT calling applyProcGroup: the child shares our group.
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	if err := terminateGroup(cmd.Process); err != nil {
		t.Fatalf("terminateGroup: %v", err)
	}
	// We are still here. Confirm the child was killed narrowly instead.
	waitFor(t, func() bool { return cmd.Process.Signal(nil) != nil },
		"the child was not stopped")
}

func countProcesses(t *testing.T, marker string) int {
	t.Helper()
	out, err := exec.Command("sh", "-c",
		"ps -A -o command= | grep -F "+marker+" | grep -v grep | wc -l").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parsing ps count %q: %v", out, err)
	}
	return n
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(killGrace + 3*time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(msg)
}
