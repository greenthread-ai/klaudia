package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// storeWithRecordedJobs builds a store holding jobs that were never launched,
// so the listing format can be checked against exact states (an exit code, a
// remote host, a restart count) without running ssh or waiting on a crash.
// No process is attached, so it must not be KillAll'd.
func storeWithRecordedJobs(t *testing.T) *JobStore {
	t.Helper()
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	s := NewJobStore(nil, "recorded")
	add := func(j *Job) {
		j.log = newJobLog(s.logDir, j.Name)
		t.Cleanup(j.log.Close)
		s.jobs[j.ID] = j
		s.order = append(s.order, j.ID)
	}
	add(&Job{ID: "bash_1", Name: "dev", Command: "npm run dev", Target: trust.LocalTarget(), port: "3000", restarts: 2})
	add(&Job{ID: "bash_2", Name: "logs", Command: "journalctl -f", Target: trust.Target{Host: "staging", Via: "ssh"}, exited: true, exitCode: 1})
	return s
}

func TestJobsToolListsStateCompactly(t *testing.T) {
	tool, _ := NewJobs(storeWithRecordedJobs(t))
	res := runTool(t, tool, Context{}, JobsInput{})
	want := `bash_1  dev  "npm run dev"  running  port 3000  restarted 2×` + "\n" +
		`bash_2  logs  "journalctl -f"  exited (1)  on ssh:staging`
	if res.Content != want {
		t.Errorf("listing =\n%s\nwant\n%s", res.Content, want)
	}
}

func TestJobsToolWithoutJobs(t *testing.T) {
	tool, _ := NewJobs(nil)
	if res := runTool(t, tool, Context{}, JobsInput{}); res.Content != "background jobs are not available" {
		t.Errorf("nil store: %q", res.Content)
	}
	tool, _ = NewJobs(newTestJobStore(t))
	if res := runTool(t, tool, Context{}, JobsInput{}); res.Content != "No background jobs." {
		t.Errorf("empty store: %q", res.Content)
	}
	if err := tool.ValidateInput(json.RawMessage(`{}`)); err != nil {
		t.Errorf("Jobs takes no arguments, but {} was rejected: %v", err)
	}
}

// Every job tool used without a store says so, as an error, rather than
// panicking or pretending the job is missing.
func TestJobToolsWithoutAStore(t *testing.T) {
	bo, _ := NewBashOutput(nil)
	ks, _ := NewKillShell(nil)
	rj, _ := NewRestartJob(nil)
	cases := []struct {
		tool Tool
		in   any
	}{
		{bo, BashOutputInput{BashID: "bash_1"}},
		{ks, KillShellInput{ShellID: "bash_1"}},
		{rj, RestartJobInput{Job: "bash_1"}},
	}
	for _, c := range cases {
		res := runTool(t, c.tool, Context{}, c.in)
		if !res.IsError || res.Content != "background jobs are not available" {
			t.Errorf("%s: res = %+v", c.tool.Name(), res)
		}
	}
}

func TestJobToolsValidateTheirReference(t *testing.T) {
	bo, _ := NewBashOutput(nil)
	ks, _ := NewKillShell(nil)
	rj, _ := NewRestartJob(nil)
	cases := []struct {
		tool    Tool
		raw     string
		wantErr string
	}{
		{bo, `{"bash_id":"dev"}`, ""},
		{bo, `{"bash_id":"  "}`, "bash_id is required"},
		{bo, `{"bash_id":"dev","filter":"("}`, "invalid filter regex"},
		{ks, `{"shell_id":"dev"}`, ""},
		{ks, `{"shell_id":""}`, "shell_id is required"},
		{rj, `{"job":"dev"}`, ""},
		{rj, `{"job":" "}`, "job is required"},
		{rj, `{}`, "job"},
	}
	for _, c := range cases {
		err := c.tool.ValidateInput(json.RawMessage(c.raw))
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s %s: unexpected error %v", c.tool.Name(), c.raw, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s %s: err = %v, want %q", c.tool.Name(), c.raw, err, c.wantErr)
		}
	}
}

// An unknown reference lists the jobs that do exist, so the model can correct
// itself in one step instead of guessing another id.
func TestJobToolsNameTheRealJobsOnAMiss(t *testing.T) {
	store := newTestJobStore(t)
	res, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	bo, _ := NewBashOutput(store)
	ks, _ := NewKillShell(store)
	rj, _ := NewRestartJob(store)
	for _, c := range []struct {
		tool Tool
		in   any
	}{
		{bo, BashOutputInput{BashID: "api"}},
		{ks, KillShellInput{ShellID: "api"}},
		{rj, RestartJobInput{Job: "api"}},
	} {
		out := runTool(t, c.tool, Context{}, c.in)
		if !out.IsError || !strings.Contains(out.Content, `No job "api"`) || !strings.Contains(out.Content, res.Job.ID) {
			t.Errorf("%s: res = %+v, want a miss naming %s", c.tool.Name(), out, res.Job.ID)
		}
	}
}

func TestUnknownJobMessageWithNoJobs(t *testing.T) {
	store := newTestJobStore(t)
	if got := store.unknownJobMsg("dev"); got != `No job "dev", and none are running.` {
		t.Errorf("msg = %q", got)
	}
}

func TestBashOutputReportsNoNewOutputWhileRunning(t *testing.T) {
	store := newTestJobStore(t)
	res, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	bo, _ := NewBashOutput(store)
	out := runTool(t, bo, Context{}, BashOutputInput{BashID: res.Job.Name})
	want := "[no new output]\n[job " + res.Job.Name + " running]"
	if out.IsError || out.Content != want {
		t.Errorf("res = %+v, want %q", out, want)
	}
}

func TestKillShellToolStopsTheJob(t *testing.T) {
	store := newTestJobStore(t)
	res, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	ks, _ := NewKillShell(store)
	out := runTool(t, ks, Context{}, KillShellInput{ShellID: res.Job.Name})
	if out.IsError || out.Content != "Stopped job "+res.Job.Name+"." {
		t.Errorf("res = %+v", out)
	}
	waitJob(t, func() bool { return !peekRunning(store, res.Job.ID) }, "KillShell did not stop the job")
}

func TestRestartJobToolKeepsTheSlot(t *testing.T) {
	store := newTestJobStore(t)
	res, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	rj, _ := NewRestartJob(store)
	out := runTool(t, rj, Context{}, RestartJobInput{Job: res.Job.ID})
	want := "Restarted job " + res.Job.Name + " (" + res.Job.ID + ")."
	if out.IsError || !strings.HasPrefix(out.Content, want) {
		t.Errorf("res = %+v, want prefix %q", out, want)
	}
	if list := store.List(); len(list) != 1 || list[0].Restarts != 1 || !list[0].Running {
		t.Errorf("after restart: %+v", list)
	}
}

func TestBashBackgroundStartsAndReusesJobs(t *testing.T) {
	store := newTestJobStore(t)
	b, _ := NewBash(sandbox.NewLocal(), store)
	dir := t.TempDir()

	first := runTool(t, b, Context{WorkingDir: dir}, BashInput{Command: "sleep 30", RunInBackground: true})
	if first.IsError || !strings.HasPrefix(first.Content, "Started job sleep (bash_1).") {
		t.Fatalf("first start = %+v", first)
	}
	second := runTool(t, b, Context{WorkingDir: dir}, BashInput{Command: "sleep 30", RunInBackground: true})
	if second.IsError || !strings.HasPrefix(second.Content, "That command is already running as job sleep (bash_1)") {
		t.Errorf("duplicate start = %+v", second)
	}
	if n := len(store.List()); n != 1 {
		t.Errorf("%d jobs, want 1", n)
	}

	// The same command in another directory is a different job.
	third := runTool(t, b, Context{WorkingDir: t.TempDir()}, BashInput{Command: "sleep 30", RunInBackground: true})
	if !strings.HasPrefix(third.Content, "Started job sleep-2 (bash_2).") {
		t.Errorf("start elsewhere = %+v", third)
	}
}

func TestBashBackgroundWithoutAStore(t *testing.T) {
	b, _ := NewBash(sandbox.NewLocal())
	res := runTool(t, b, Context{}, BashInput{Command: "sleep 30", RunInBackground: true})
	if !res.IsError || res.Content != "background execution is not available" {
		t.Errorf("res = %+v", res)
	}
}

// The first port a job announces is what the Jobs listing shows; it is noticed
// as output is read, not guessed from the command.
func TestPortIsNotedFromOutputOnRead(t *testing.T) {
	store := newTestJobStore(t)
	res, err := store.Start(sandbox.NewLocal(), sandbox.Request{Command: "echo 'listening on http://localhost:4321'; echo 'port 9999'; sleep 30"})
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	waitJob(t, func() bool {
		out, _ := store.Read(res.Job.ID)
		seen += out.Output
		return strings.Contains(seen, "9999")
	}, "the job's output never arrived")
	if p := store.List()[0].Port; p != "4321" {
		t.Errorf("port = %q, want the first one announced (4321)", p)
	}
}

func TestJobLogFallsBackToBoundedMemory(t *testing.T) {
	// A directory that cannot be created (its parent is a file) leaves the log
	// memory-only, which must still work and must not grow without bound.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	l := newJobLog(filepath.Join(blocker, "jobs"), "dev")
	if l.Path() != "" {
		t.Fatalf("path = %q, want memory-only", l.Path())
	}
	l.Write([]byte("one\ntwo\n"))
	if got, off := l.ReadFrom(4); got != "two\n" || off != 8 {
		t.Errorf("ReadFrom(4) = %q, %d", got, off)
	}

	big := strings.Repeat("x", maxMemLog)
	l.Write([]byte(big))
	l.Write([]byte("\ntail-line\n"))
	if l.Size() != maxMemLog {
		t.Errorf("size = %d, want it capped at %d", l.Size(), maxMemLog)
	}
	all, _ := l.ReadFrom(0)
	if strings.Contains(all, "one") || !strings.HasSuffix(all, "tail-line\n") {
		t.Error("the memory log did not keep the tail")
	}
	l.Close() // no file to close; must not panic
}

func TestJobLogReadsAndTails(t *testing.T) {
	l := newJobLog(t.TempDir(), "dev")
	t.Cleanup(l.Close)
	if l.Path() == "" {
		t.Fatal("expected a file-backed log")
	}
	l.Write([]byte("a\nb\nc\n"))
	l.note("restarted (%s)", "now")

	if got := l.Tail(2); got != "\n── klaudia: restarted (now) ──" {
		t.Errorf("Tail(2) = %q", got)
	}
	if got := l.Tail(100); !strings.HasPrefix(got, "a\nb\nc\n") {
		t.Errorf("Tail(100) = %q, want everything", got)
	}
	// An offset past the end (or negative) reads nothing rather than failing.
	for _, off := range []int64{-1, l.Size() + 10} {
		if got, n := l.ReadFrom(off); got != "" || n != l.Size() {
			t.Errorf("ReadFrom(%d) = %q, %d", off, got, n)
		}
	}
	data, _ := os.ReadFile(l.Path())
	if !strings.HasPrefix(string(data), "a\nb\nc\n") {
		t.Errorf("file = %q", data)
	}
}

func TestPruneJobLogsRemovesOnlyOldSessions(t *testing.T) {
	root := t.TempDir()
	old, fresh := filepath.Join(root, "old"), filepath.Join(root, "fresh")
	for _, d := range []string{old, fresh} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	pruneJobLogs(root, jobLogRetention)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("an expired session's logs were kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a current session's logs were removed")
	}
	pruneJobLogs(filepath.Join(root, "missing"), time.Hour) // must not panic
}

func TestJobLogDirHonoursConfigDirAndDefaultsSession(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", cfg)
	if got := jobLogDir(""); got != filepath.Join(cfg, "jobs", "default") {
		t.Errorf("jobLogDir(\"\") = %q", got)
	}
	home := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", "")
	t.Setenv("HOME", home)
	if got := jobLogDir("s1"); got != filepath.Join(home, ".klaudia", "jobs", "s1") {
		t.Errorf("jobLogDir without config dir = %q", got)
	}
}

func TestJobFormattingHelpers(t *testing.T) {
	for d, want := range map[time.Duration]string{
		42 * time.Second:                "42s",
		5 * time.Minute:                 "5m",
		2*time.Hour + 7*time.Minute:     "2h7m",
		26*time.Hour + 59*time.Minute:   "26h59m",
		500 * time.Millisecond:          "0s",
		59*time.Minute + 59*time.Second: "59m",
	} {
		if got := fmtShortDuration(d); got != want {
			t.Errorf("fmtShortDuration(%v) = %q, want %q", d, got, want)
		}
	}

	for in, want := range map[string]string{
		"dev":                                 "dev",
		"  my server!  ":                      "my-server",
		"!!!":                                 "job",
		"..":                                  "job",
		"a-really-long-script-name-for-tests": "a-really-long-script-nam",
	} {
		if got := sanitiseName(in); got != want {
			t.Errorf("sanitiseName(%q) = %q, want %q", in, got, want)
		}
	}

	if got := oneLineCommand("npm   run\n dev", 30); got != "npm run dev" {
		t.Errorf("oneLineCommand collapses whitespace: %q", got)
	}
	if got := oneLineCommand("abcdefghij", 5); got != "abcd…" {
		t.Errorf("oneLineCommand truncates: %q", got)
	}

	jobs := []JobStatus{{Name: "web"}, {Name: "api"}, {Name: "dev"}}
	sortJobsByName(jobs)
	if jobs[0].Name != "api" || jobs[1].Name != "dev" || jobs[2].Name != "web" {
		t.Errorf("sortJobsByName = %v", jobs)
	}

	if got := RenderJobs([]JobStatus{{ID: "bash_1", Name: "logs", Command: "x", Where: "ssh:staging"}}); !strings.Contains(got, "@ssh:staging") {
		t.Errorf("a remote job's host is not shown:\n%s", got)
	}
	if got := RenderJobs([]JobStatus{{ID: "bash_1", Name: "t", Command: "true", Where: "local"}}); !strings.Contains(got, "exited") || strings.Contains(got, "crashed") {
		t.Errorf("a clean exit should read as exited:\n%s", got)
	}
	if got := jobWhere(trust.Target{Host: "build"}); got != "build" {
		t.Errorf("jobWhere without a via = %q", got)
	}
}
