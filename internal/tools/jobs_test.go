package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

func waitJob(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal(msg)
}

// Nothing used to notice a job dying. A crashed dev server stayed "running"
// until the model happened to read it, so "why is the site down" got the answer
// "it's running fine".
func TestExitIsNoticedWithoutAnyoneReading(t *testing.T) {
	store := newTestJobStore(t)
	got := make(chan JobStatus, 1)
	store.OnExit(func(st JobStatus) { got <- st })

	if _, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "exit 3"}); err != nil {
		t.Fatal(err)
	}
	select {
	case st := <-got:
		if st.Running {
			t.Error("the exit callback reported the job as still running")
		}
		if st.ExitCode != 3 {
			t.Errorf("exit code = %d, want 3", st.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing reported the job exiting")
	}
}

// Two dev servers fighting over one port is a confusing failure, and the loser
// looks like broken code.
func TestDuplicateCommandReusesTheRunningJob(t *testing.T) {
	store := newTestJobStore(t)
	first, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate {
		t.Error("a second identical command was not flagged as a duplicate")
	}
	if second.Job.ID != first.Job.ID {
		t.Errorf("got a second job %s; want the existing %s", second.Job.ID, first.Job.ID)
	}
	if n := len(store.List()); n != 1 {
		t.Errorf("%d jobs tracked, want 1", n)
	}

	// A command that only *looks* similar is still its own job.
	third, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 31"})
	if third.Duplicate {
		t.Error("a different command was treated as a duplicate")
	}
}

// An exited job's command is free again: the guard must not block a restart of
// something that already stopped.
func TestDuplicateGuardIgnoresExitedJobs(t *testing.T) {
	store := newTestJobStore(t)
	first, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "true"})
	waitJob(t, func() bool {
		for _, j := range store.List() {
			if j.ID == first.Job.ID {
				return !j.Running
			}
		}
		return false
	}, "the job never exited")

	second, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "true"})
	if second.Duplicate {
		t.Error("an exited job blocked starting the same command again")
	}
}

// "Restart the API" has to act on the API, not produce a second one.
func TestRestartKeepsIdentityAndReplacesTheProcess(t *testing.T) {
	store := newTestJobStore(t)
	res, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	before := res.Job

	st, ok := store.Restart(before.Name)
	if !ok {
		t.Fatal("restart failed")
	}
	if st.ID != before.ID || st.Name != before.Name {
		t.Errorf("restart changed identity: %s/%s → %s/%s", before.ID, before.Name, st.ID, st.Name)
	}
	if st.Restarts != 1 {
		t.Errorf("restart count = %d, want 1", st.Restarts)
	}
	if !st.Running {
		t.Error("the job is not running after a restart")
	}
	if n := len(store.List()); n != 1 {
		t.Errorf("%d jobs after restart, want 1 — a restart must not leave the old one behind", n)
	}
	if st.LogPath != before.LogPath {
		t.Error("the restart moved the log file, so /logs would lose the history")
	}
}

// Jobs are addressable by the name a person would say, not only by bash_N.
func TestJobsAreFoundByNameOrID(t *testing.T) {
	store := newTestJobStore(t)
	res, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	for _, ref := range []string{res.Job.ID, res.Job.Name, strings.ToUpper(res.Job.Name)} {
		if _, ok := store.Read(ref); !ok {
			t.Errorf("job not found by %q", ref)
		}
	}
	if _, ok := store.Read("nope"); ok {
		t.Error("an unknown reference resolved to a job")
	}
}

// A bare "no such job" makes the model invent another id. Listing the real ones
// lets it correct itself in one step.
func TestUnknownJobMessageNamesTheRealOnes(t *testing.T) {
	store := newTestJobStore(t)
	store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	msg := store.unknownJobMsg("api")
	if !strings.Contains(msg, "sleep") && !strings.Contains(msg, "bash_1") {
		t.Errorf("message does not name the jobs that exist: %q", msg)
	}

	empty := newTestJobStore(t)
	if !strings.Contains(empty.unknownJobMsg("api"), "none are running") {
		t.Errorf("with no jobs, the message should say so: %q", empty.unknownJobMsg("api"))
	}
}

// Output goes to a file, so it can be paged and survives the process that made
// it. Before, it lived in a slice that only grew.
func TestOutputIsWrittenToDisk(t *testing.T) {
	store := newTestJobStore(t)
	res, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "printf 'line one\\nline two\\n'"})
	waitJob(t, func() bool { return store.List()[0].LogBytes > 0 }, "nothing was logged")

	text, path, ok := store.Log(res.Job.Name)
	if !ok {
		t.Fatal("log not found")
	}
	if !strings.Contains(text, "line one") || !strings.Contains(text, "line two") {
		t.Errorf("log text = %q", text)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.Contains(string(onDisk), "line two") {
		t.Errorf("file contents = %q", onDisk)
	}
}

// Reads are incremental, and a restart must not replay the whole log.
func TestReadsAreIncrementalAcrossRestart(t *testing.T) {
	store := newTestJobStore(t)
	res, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "printf 'first\\n'; sleep 30"})
	waitJob(t, func() bool {
		out, _ := store.Read(res.Job.Name)
		return strings.Contains(out.Output, "first")
	}, "first output never arrived")

	out, _ := store.Read(res.Job.Name)
	if strings.Contains(out.Output, "first") {
		t.Error("the second read replayed output already returned")
	}

	store.Restart(res.Job.Name)
	waitJob(t, func() bool {
		o, _ := store.Read(res.Job.Name)
		return strings.Contains(o.Output, "first")
	}, "output after restart never arrived")

	// The pre-restart history is still on disk for /logs, even though reads
	// resumed after it.
	all, _, _ := store.Log(res.Job.Name)
	if strings.Count(all, "first") != 2 {
		t.Errorf("the log should hold both runs, got %q", all)
	}
	if !strings.Contains(all, "restarted") {
		t.Error("the log does not mark where the restart happened")
	}
}

func TestPortDetection(t *testing.T) {
	for _, tc := range []struct{ line, want string }{
		{"Local:   http://localhost:5173/", "5173"},
		{"Listening on port 8080", "8080"},
		{"server started on :3000", "3000"},
		{"Serving HTTP on 0.0.0.0 port 8000 ...", "8000"},
		{"ready - started server on http://127.0.0.1:3001", "3001"},
		{"compiled successfully in 431ms", ""}, // not a port
		{"2026-08-24 10:15:33 starting", ""},   // not a port
	} {
		if got := detectPort(tc.line); got != tc.want {
			t.Errorf("detectPort(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

// Names are derived from what the user already calls these things, not mapped
// from a table that guesses at their vocabulary.
func TestJobNames(t *testing.T) {
	for cmd, want := range map[string]string{
		"npm run dev":                      "dev",
		"pnpm run build:watch":             "build-watch",
		"yarn dev":                         "dev",
		"make dev-api":                     "dev-api",
		"docker compose up -d":             "compose",
		"go run ./cmd/server":              "server",
		"python -m http.server 8000":       "http.server",
		"cargo watch -x run":               "watch",
		"./scripts/start.sh":               "start.sh",
		"/usr/local/bin/postgres -D /data": "postgres",
		"":                                 "job",
	} {
		if got := jobName(cmd); got != want {
			t.Errorf("jobName(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestJobNamesAreUnique(t *testing.T) {
	store := newTestJobStore(t)
	a, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	b, _ := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 31"})
	if a.Job.Name == b.Job.Name {
		t.Fatalf("both jobs are called %q", a.Job.Name)
	}
	if b.Job.Name != a.Job.Name+"-2" {
		t.Errorf("second name = %q, want %q", b.Job.Name, a.Job.Name+"-2")
	}
}

// §11: where a job runs has to be clear. The trust classifier already extracts
// the host, so a job started over ssh says so.
func TestJobLocationIsRecorded(t *testing.T) {
	for cmd, want := range map[string]string{
		"npm run dev":                       "local",
		"ssh staging 'journalctl -fu api'":  "ssh:staging",
		"docker compose up":                 "local", // compose runs here, containers are its business
		"kubectl --context prod logs -f po": "kubectl:prod",
	} {
		if got := jobWhere(jobTarget(cmd)); got != want {
			t.Errorf("job location for %q = %q, want %q", cmd, got, want)
		}
	}
}

func TestKillAllStopsEverything(t *testing.T) {
	store := newTestJobStore(t)
	store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 31"})
	store.KillAll()
	waitJob(t, func() bool {
		for _, j := range store.List() {
			if j.Running {
				return false
			}
		}
		return true
	}, "a job survived KillAll")
}

func TestRenderJobs(t *testing.T) {
	if out := RenderJobs(nil); !strings.Contains(out, "No jobs") {
		t.Errorf("empty table = %q", out)
	}
	out := RenderJobs([]JobStatus{
		{ID: "bash_1", Name: "dev", Command: "npm run dev", Running: true, Port: "3000", Where: "local"},
		{ID: "bash_2", Name: "api", Command: "make dev-api", Running: false, ExitCode: 1, Where: "local", Restarts: 2},
	})
	for _, want := range []string{"dev", "npm run dev", "running", ":3000", "api", "crashed (1)", "×2 restarts"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

var _ = context.Background

// A bare "exit 124" reads as "the command is broken". Usually it means the
// command has no natural end and should have been a job.
func TestTimeoutSuggestsAJobForServices(t *testing.T) {
	out, _ := formatBashOutput(sandbox.Response{
		Stdout: "listening\n", TimedOut: true, ExitCode: 124,
	}, "npm run dev")
	if !strings.Contains(out, "run_in_background") {
		t.Errorf("a timed-out service does not suggest making it a job:\n%s", out)
	}
	if !strings.Contains(out, "Jobs") {
		t.Errorf("the suggestion does not mention checking for an existing job:\n%s", out)
	}

	// A test suite that genuinely hung must not be told to background itself.
	slow, _ := formatBashOutput(sandbox.Response{
		Stdout: "…\n", TimedOut: true, ExitCode: 124,
	}, "go test ./...")
	if strings.Contains(slow, "run_in_background") {
		t.Errorf("a slow test was told to become a service:\n%s", slow)
	}
}

func TestLooksLikeService(t *testing.T) {
	for _, cmd := range []string{
		"npm run dev", "yarn start", "docker compose up", "python -m http.server",
		"tail -f /var/log/app.log", "make dev", "uvicorn app:main --reload",
	} {
		if !looksLikeService(cmd) {
			t.Errorf("%q should look like a service", cmd)
		}
	}
	for _, cmd := range []string{
		"go test ./...", "npm run build", "make test", "git log",
		"tail -f app.log | head -20", // a pipeline that ends
		"npm run lint",
	} {
		if looksLikeService(cmd) {
			t.Errorf("%q should not look like a service", cmd)
		}
	}
}

// A process that ignores SIGTERM must still be gone when the session ends.
//
// This is the case KillAll used to leak: it sent the signal and returned, the
// binary exited, and the goroutine that would have sent SIGKILL two seconds
// later went with it — so the server kept its port forever.
func TestKillAllWaitsOutAStubbornProcess(t *testing.T) {
	store := newTestJobStore(t)
	marker := "klaudia-stubborn-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	// trap '' TERM ignores SIGTERM entirely; only SIGKILL will end this.
	_, err := store.Start(sandbox.NewLocal(), sandbox.Request{
		Command: fmt.Sprintf("trap '' TERM; sh -c 'sleep 60 # %s' & wait", marker),
	})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, func() bool { return countMarker(t, marker) > 0 }, "the process never started")

	store.KillAll()
	// KillAll has returned, so by its own contract the process is gone. No
	// polling here on purpose: the whole bug was returning too early.
	if n := countMarker(t, marker); n != 0 {
		t.Fatalf("%d process(es) survived KillAll — a session exiting now would leak them", n)
	}
}

func countMarker(t *testing.T, marker string) int {
	t.Helper()
	out, err := exec.Command("sh", "-c",
		"ps -A -o command= | grep -F "+marker+" | grep -v grep | wc -l").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}
