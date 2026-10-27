package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// Long-running jobs (a VM provisioning, a rollout): the model can wait on one
// in a single call, its exit says whether it was stopped or finished, and
// stopping it takes its whole process tree with it.

func TestWaitReturnsOnExitOrMatch(t *testing.T) {
	store := newTestJobStore(t)
	defer func(d time.Duration) { waitPoll = d }(waitPoll)
	waitPoll = 20 * time.Millisecond

	st, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 0.3; echo provisioned"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	why, ok := store.Wait(context.Background(), st.Job.ID, nil, 10*time.Second)
	if !ok || why != "exited" || time.Since(start) > 5*time.Second {
		t.Fatalf("Wait = %q %v after %s", why, ok, time.Since(start))
	}

	st, _ = store.Start(sandbox.NewLocal(), sandbox.Request{Command: "echo booting; sleep 0.2; echo READY; sleep 30"})
	why, _ = store.Wait(context.Background(), st.Job.ID, regexp.MustCompile("READY"), 10*time.Second)
	if why != "matched" {
		t.Errorf("Wait for READY = %q", why)
	}
	why, _ = store.Wait(context.Background(), st.Job.ID, regexp.MustCompile("NEVER"), 100*time.Millisecond)
	if why != "timed out" {
		t.Errorf("Wait with no match = %q", why)
	}
	store.Kill(st.Job.ID)
}

// One BashOutput call waits out the job and reports how it ended.
func TestBashOutputWaits(t *testing.T) {
	store := newTestJobStore(t)
	st, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 0.3; echo done-provisioning"})
	if err != nil {
		t.Fatal(err)
	}
	tool, _ := NewBashOutput(store)
	raw, _ := json.Marshal(BashOutputInput{BashID: st.Job.ID, WaitSeconds: 10})
	if err := tool.ValidateInput(raw); err != nil {
		t.Fatal(err)
	}
	res, err := tool.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res[0].Content, "done-provisioning") || !strings.Contains(res[0].Content, "exited, code 0") {
		t.Errorf("result = %q", res[0].Content)
	}
	bad, _ := json.Marshal(BashOutputInput{BashID: st.Job.ID, WaitSeconds: 601})
	if tool.ValidateInput(bad) == nil {
		t.Error("wait_seconds above the cap was accepted")
	}
}

// Quiet is reported in the running line, so the model can judge it.
func TestJobStateLineReportsQuiet(t *testing.T) {
	now := time.Now()
	got := jobStateLine(ShellOutput{Name: "provision", Running: true,
		Started: now.Add(-42 * time.Minute), LastOutput: now.Add(-25 * time.Minute)}, now)
	if got != "[job provision running for 42m, last output 25m ago]" {
		t.Errorf("got %q", got)
	}
	got = jobStateLine(ShellOutput{Name: "p", Running: true, Started: now.Add(-3 * time.Hour)}, now)
	if got != "[job p running for 3h00m, no output yet]" {
		t.Errorf("got %q", got)
	}
}

// A stop on request is marked, so the frontend does not hand it to the model
// as news; a job that ends on its own is not.
func TestExitSaysWhetherItWasStopped(t *testing.T) {
	store := newTestJobStore(t)
	got := make(chan JobStatus, 2)
	store.OnExit(func(st JobStatus) { got <- st })
	if _, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "true"}); err != nil {
		t.Fatal(err)
	}
	if st := <-got; st.Stopped {
		t.Error("a job that finished on its own was marked stopped")
	}
	st, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	store.Kill(st.Job.ID)
	select {
	case st := <-got:
		if !st.Stopped {
			t.Error("a killed job was not marked stopped")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the killed job never exited")
	}
}

// Stopping a job takes its children too: a provisioning script's ssh or
// terraform must not outlive the job that started it.
func TestKillTakesTheWholeProcessTree(t *testing.T) {
	store := newTestJobStore(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	st, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 60 & echo $! > " + pidFile + "; wait"})
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	waitJob(t, func() bool {
		b, err := os.ReadFile(pidFile)
		if err != nil || len(strings.TrimSpace(string(b))) == 0 {
			return false
		}
		_, err = fmt.Sscan(strings.TrimSpace(string(b)), &pid)
		return err == nil
	}, "the child never started")
	store.Kill(st.Job.ID)
	waitJob(t, func() bool { return syscall.Kill(pid, 0) != nil }, "the grandchild outlived its job")
}
