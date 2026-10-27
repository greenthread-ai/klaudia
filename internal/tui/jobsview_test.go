package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/agent"
)

type fakeJobs struct {
	jobs     []tools.JobStatus
	logs     map[string]string
	reads    map[string]tools.ShellOutput
	killed   []string
	restarts []string
	onExit   func(tools.JobStatus)
}

func (f *fakeJobs) List() []tools.JobStatus         { return f.jobs }
func (f *fakeJobs) OnExit(fn func(tools.JobStatus)) { f.onExit = fn }
func (f *fakeJobs) Log(ref string) (string, string, bool) {
	t, ok := f.logs[ref]
	return t, "", ok
}
func (f *fakeJobs) Read(ref string) (tools.ShellOutput, bool) {
	o, ok := f.reads[ref]
	return o, ok
}
func (f *fakeJobs) Kill(ref string) bool { f.killed = append(f.killed, ref); return true }
func (f *fakeJobs) Restart(ref string) (tools.JobStatus, bool) {
	f.restarts = append(f.restarts, ref)
	return tools.JobStatus{ID: "bash_1", Name: ref, Running: true}, true
}

func (f *fakeJobs) ExitNotice(st tools.JobStatus) string {
	return fmt.Sprintf("<background-job id=%q>\nBackground job %s (%s) exited with code %d.\n</background-job>",
		st.ID, st.ID, st.Command, st.ExitCode)
}

func (f *fakeJobs) OnMatch(func(tools.JobStatus, []string, int)) {}

func (f *fakeJobs) MatchNotice(st tools.JobStatus, lines []string, _ int) string {
	return fmt.Sprintf("<background-job-output id=%q>\n%s\n</background-job-output>", st.ID, strings.Join(lines, "\n"))
}

func jobsModel(t *testing.T, f *fakeJobs) *Model {
	t.Helper()
	// A turn in flight, so an exit is queued for the model rather than
	// starting a turn of its own (which needs a full Model).
	return &Model{sess: &Session{Jobs: f}, height: 40, state: stateRunning}
}

// A crash has to be reported when it happens, not when someone next looks.
func TestJobExitIsReported(t *testing.T) {
	m := jobsModel(t, &fakeJobs{})
	m.onJobExit(tools.JobStatus{Name: "api", ExitCode: 1})
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "api") || !strings.Contains(out, "code 1") {
		t.Errorf("crash was not reported usefully:\n%s", out)
	}
	if !strings.Contains(out, "/logs api") {
		t.Errorf("the report does not say how to look:\n%s", out)
	}
}

// A clean exit is not a failure and must not read like one.
func TestCleanExitIsNotAnError(t *testing.T) {
	m := jobsModel(t, &fakeJobs{})
	m.onJobExit(tools.JobStatus{Name: "build", ExitCode: 0})
	out := stripANSI(m.transcript.String())
	if strings.Contains(out, "code 0") {
		t.Errorf("a clean exit was reported as a code:\n%s", out)
	}
	if !strings.Contains(out, "cleanly") {
		t.Errorf("output = %s", out)
	}
}

// Esc while following a log stops watching. It must not kill the job, and it
// must not be mistaken for "interrupt the turn".
func TestStopFollowLeavesTheJobRunning(t *testing.T) {
	f := &fakeJobs{}
	m := jobsModel(t, f)
	m.following = "api"
	if !m.stopFollow() {
		t.Fatal("stopFollow reported nothing to stop")
	}
	if m.following != "" {
		t.Error("still following")
	}
	if len(f.killed) != 0 {
		t.Errorf("stopping follow killed jobs: %v", f.killed)
	}
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "still running") {
		t.Errorf("the user is not told the job survived:\n%s", out)
	}
	if m.stopFollow() {
		t.Error("stopFollow reported success when nothing was being followed")
	}
}

// Following a job that dies stops on its own rather than ticking forever.
func TestFollowStopsWhenTheJobExits(t *testing.T) {
	f := &fakeJobs{reads: map[string]tools.ShellOutput{
		"api": {Output: "bye\n", Running: false, ExitCode: 2},
	}}
	m := jobsModel(t, f)
	m.following = "api"
	if cmd := m.onFollowTick("api"); cmd != nil {
		t.Error("follow scheduled another tick after the job exited")
	}
	if m.following != "" {
		t.Error("still following an exited job")
	}
}

// A tick for a job we are no longer following must do nothing — otherwise
// switching between two logs interleaves them.
func TestStaleFollowTickIsIgnored(t *testing.T) {
	f := &fakeJobs{reads: map[string]tools.ShellOutput{"old": {Output: "noise\n", Running: true}}}
	m := jobsModel(t, f)
	m.following = "new"
	if cmd := m.onFollowTick("old"); cmd != nil {
		t.Error("a stale follow tick kept ticking")
	}
	if out := m.transcript.String(); strings.Contains(out, "noise") {
		t.Errorf("a stale tick printed into the transcript:\n%s", out)
	}
}

// §12: promote a failure into the conversation without dumping the whole log.
func TestErrorLinesKeepStackTracesWhole(t *testing.T) {
	log := strings.Join([]string{
		"GET /health 200 1ms",
		"GET /health 200 1ms",
		"Error: connect ECONNREFUSED 127.0.0.1:5432",
		"    at TCPConnectWrap.afterConnect",
		"    at process._tickCallback",
		"GET /health 200 1ms",
		"listening on :3000",
	}, "\n")
	got := errorLines(log)
	joined := strings.Join(got, "\n")

	if !strings.Contains(joined, "ECONNREFUSED") {
		t.Errorf("the error line was dropped:\n%s", joined)
	}
	if !strings.Contains(joined, "afterConnect") || !strings.Contains(joined, "_tickCallback") {
		t.Errorf("the stack trace was cut off after its first line:\n%s", joined)
	}
	if strings.Contains(joined, "GET /health") {
		t.Errorf("routine request logging was promoted too:\n%s", joined)
	}
	if strings.Contains(joined, "listening on") {
		t.Errorf("an unrelated line after the trace was swept in:\n%s", joined)
	}
}

func TestErrorLinesAreBounded(t *testing.T) {
	log := strings.Repeat("fatal: nope\n", 1000)
	got := errorLines(log)
	if len(got) > 205 {
		t.Errorf("promoted %d lines; a log of nothing but errors would flood the context", len(got))
	}
	if !strings.Contains(got[len(got)-1], "truncated") {
		t.Error("truncation is not announced")
	}
}

// With one job running, naming it every time is noise.
func TestSoleRunningJobIsTheDefault(t *testing.T) {
	one := &fakeJobs{jobs: []tools.JobStatus{{Name: "dev", Running: true}}}
	if got := soleRunningJob(one); got != "dev" {
		t.Errorf("sole running job = %q, want dev", got)
	}
	two := &fakeJobs{jobs: []tools.JobStatus{
		{Name: "dev", Running: true}, {Name: "api", Running: true},
	}}
	if got := soleRunningJob(two); got != "" {
		t.Errorf("with two running jobs there is a choice to make, got %q", got)
	}
	stopped := &fakeJobs{jobs: []tools.JobStatus{
		{Name: "dev", Running: true}, {Name: "api", Running: false},
	}}
	if got := soleRunningJob(stopped); got != "dev" {
		t.Errorf("an exited job should not count as a choice, got %q", got)
	}
}

func TestStopJobAll(t *testing.T) {
	f := &fakeJobs{jobs: []tools.JobStatus{
		{ID: "bash_1", Name: "dev", Running: true},
		{ID: "bash_2", Name: "api", Running: true},
		{ID: "bash_3", Name: "old", Running: false},
	}}
	m := jobsModel(t, f)
	m.stopJobCommand([]string{"all"})
	if len(f.killed) != 2 {
		t.Errorf("killed %v; want the two running jobs only", f.killed)
	}
}

func TestRestartCommandUsesTheSoleJob(t *testing.T) {
	f := &fakeJobs{jobs: []tools.JobStatus{{ID: "bash_1", Name: "dev", Running: true}}}
	m := jobsModel(t, f)
	m.restartCommand(nil)
	if len(f.restarts) != 1 || f.restarts[0] != "dev" {
		t.Errorf("restarts = %v, want [dev]", f.restarts)
	}
}

func TestJobsWithoutAStoreSaysSo(t *testing.T) {
	m := &Model{sess: &Session{}}
	m.jobsCommand()
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "not available") {
		t.Errorf("output = %s", out)
	}
}

// A job that ends on its own is news for the model, as a background agent's
// result is: left to provision something, it must be able to wake the model
// rather than wait to be polled. One stopped on request is not.
func TestJobExitReachesTheModelUnlessStopped(t *testing.T) {
	f := &fakeJobs{}
	m := jobsModel(t, f)
	m.onJobExit(tools.JobStatus{ID: "bash_2", Name: "provision", Command: "./provision.sh", ExitCode: 0})
	in := m.steer.drain()
	for _, want := range []string{`<background-job id="bash_2"`, "provision.sh", "exited with code 0"} {
		if !strings.Contains(in.Text, want) {
			t.Errorf("notice lacks %q:\n%s", want, in.Text)
		}
	}

	m.onJobExit(tools.JobStatus{ID: "bash_3", Name: "dev", Stopped: true})
	if m.steer.pending() {
		t.Error("a job stopped on request was delivered to the model")
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "job dev stopped") {
		t.Errorf("the stop was not reported to the user:\n%s", out)
	}
}

// Idle, the exit starts a turn — the model wakes on its own.
func TestJobExitWakesAnIdleSession(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	m.ctx = t.Context()
	m.sess.Jobs = &fakeJobs{}
	prompts := make(chan string, 1)
	m.run = func(_ context.Context, turn agent.Turn) (agent.Result, error) {
		prompts <- turn.Prompt
		return agent.Result{}, nil
	}
	if cmd := m.onJobExit(tools.JobStatus{ID: "bash_1", Name: "rollout", ExitCode: 1}); cmd == nil {
		t.Fatal("idle: want a turn started")
	}
	select {
	case p := <-prompts:
		if !strings.Contains(p, `<background-job id="bash_1"`) {
			t.Errorf("turn prompt = %q", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no turn ran")
	}
}

// Quitting stops every job; with one running, it says so first.
func TestQuitWarnsAboutRunningJobs(t *testing.T) {
	m := newTestModel()
	m.sess.Jobs = &fakeJobs{jobs: []tools.JobStatus{{ID: "bash_1", Name: "provision", Running: true, Started: time.Now().Add(-42 * time.Minute)}}}
	if _, cmd := m.handleSlash("/quit"); cmd != nil || m.quitting {
		t.Fatal("quit with a job running did not warn first")
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "Quitting stops provision (42m") {
		t.Errorf("warning = %q", out)
	}
	if _, cmd := m.handleSlash("/quit"); cmd == nil || !m.quitting {
		t.Error("the second /quit did not quit")
	}
	m2 := newTestModel()
	m2.sess.Jobs = m.sess.Jobs
	m2.quitArmed = true
	if got := visibleText(m2.statusLineAt(200)); !strings.Contains(got, "stops provision") {
		t.Errorf("Ctrl+C hint = %q", got)
	}
}

// A watched job's lines go to the model as an exit does.
func TestJobMatchReachesTheModel(t *testing.T) {
	m := jobsModel(t, &fakeJobs{})
	m.onJobMatch(jobMatchMsg{status: tools.JobStatus{ID: "bash_1", Name: "dev", Watch: "ERROR"}, lines: []string{"ERROR: db down"}})
	if in := m.steer.drain(); !strings.Contains(in.Text, "ERROR: db down") {
		t.Errorf("notice = %q", in.Text)
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "job dev: 1 line matching /ERROR/") {
		t.Errorf("user line = %q", out)
	}
}
