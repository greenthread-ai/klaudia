package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// fakeBin puts inert executables with the given names on a PATH of their own,
// so pager and editor lookups resolve without launching anything real. The
// returned commands are never run: tea.ExecProcess only describes the process.
func fakeBin(t *testing.T, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("PAGER", "")
	t.Setenv("LESS", "")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	t.Setenv("TMPDIR", t.TempDir())
}

// logJobs is a job store with one running job and a log for it.
func logJobs(log string) *fakeJobs {
	return &fakeJobs{
		jobs: []tools.JobStatus{{ID: "bash_1", Name: "api", Running: true}},
		logs: map[string]string{"api": log},
	}
}

func TestLogsWithoutANameListsJobsWhenThereIsAChoice(t *testing.T) {
	f := &fakeJobs{jobs: []tools.JobStatus{
		{ID: "bash_1", Name: "api", Running: true},
		{ID: "bash_2", Name: "web", Running: true},
	}}
	m := jobsModel(t, f)
	m.logsCommand(nil)
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "usage: /logs [-f] [--errors] <job>") || !strings.Contains(out, "web") {
		t.Errorf("an ambiguous /logs should list the jobs:\n%s", out)
	}
}

func TestLogsOfAnUnknownJob(t *testing.T) {
	m := jobsModel(t, logJobs("x"))
	m.logsCommand([]string{"nope"})
	if !strings.Contains(stripANSI(m.transcript.String()), "no job nope") {
		t.Errorf("unknown job:\n%s", stripANSI(m.transcript.String()))
	}
}

func TestLogsOfASilentJob(t *testing.T) {
	m := jobsModel(t, logJobs("  \n"))
	m.logsCommand(nil) // the sole running job is the default
	if !strings.Contains(stripANSI(m.transcript.String()), "api has produced no output yet") {
		t.Errorf("silent job:\n%s", stripANSI(m.transcript.String()))
	}
}

func TestLogsFallsBackToTheTailWithoutAPager(t *testing.T) {
	fakeBin(t) // nothing on PATH
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line "+string(rune('a'+i%26)))
	}
	lines[49] = "the very last line"
	m := jobsModel(t, logJobs(strings.Join(lines, "\n")+"\n"))
	m.height = 5
	_, cmd := m.logsCommand([]string{"api"})
	if cmd != nil {
		t.Error("with no pager there is nothing to exec")
	}
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "the very last line") {
		t.Errorf("the tail was not printed:\n%s", out)
	}
	if got := strings.Count(strings.TrimSpace(out), "\n") + 1; got != 5 {
		t.Errorf("printed %d lines, want the last 5:\n%s", got, out)
	}
}

func TestLogsHandsTheLogToThePager(t *testing.T) {
	fakeBin(t, "less")
	f := logJobs("GET / 200\n")
	m := jobsModel(t, f)

	// No file behind the log: the text is paged from a temp file.
	if _, cmd := m.logsCommand([]string{"api"}); cmd == nil {
		t.Error("a pager was available but not used")
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "klaudia-logs-api-*.txt"))
	if len(matches) != 1 {
		t.Fatalf("temp files = %v", matches)
	}
	if data, _ := os.ReadFile(matches[0]); string(data) != "GET / 200\n" {
		t.Errorf("paged text = %q", data)
	}
}

// pathJobs reports a log file path, which the pager is given directly.
type pathJobs struct {
	*fakeJobs
	path string
}

func (p pathJobs) Log(ref string) (string, string, bool) {
	t, _, ok := p.fakeJobs.Log(ref)
	return t, p.path, ok
}

func TestLogsPagesTheRealLogFile(t *testing.T) {
	fakeBin(t, "less")
	m := jobsModel(t, nil)
	m.sess.Jobs = pathJobs{fakeJobs: logJobs("GET / 200\n"), path: "/var/log/api.log"}
	if _, cmd := m.logsCommand([]string{"api"}); cmd == nil {
		t.Error("the log file was not handed to the pager")
	}
	if matches, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "klaudia-*")); len(matches) != 0 {
		t.Errorf("a temp copy was made of a log that has a file: %v", matches)
	}
}

func TestLogsFollowPrintsWhatIsThereAndTicks(t *testing.T) {
	f := logJobs("")
	f.reads = map[string]tools.ShellOutput{"api": {Output: "booting\nlistening on :3000\n", Running: true}}
	m := jobsModel(t, f)
	_, cmd := m.logsCommand([]string{"-f", "api"})
	if cmd == nil {
		t.Fatal("follow did not schedule a poll")
	}
	if m.following != "api" {
		t.Errorf("following = %q", m.following)
	}
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "Following api") || !strings.Contains(out, "listening on :3000") {
		t.Errorf("follow start:\n%s", out)
	}
	// The next tick prints new output and keeps going while the job runs.
	f.reads["api"] = tools.ShellOutput{Output: "GET /health 200\n", Running: true}
	if next := m.onFollowTick("api"); next == nil {
		t.Error("follow stopped while the job was still running")
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "GET /health 200") {
		t.Errorf("tick output:\n%s", stripANSI(m.transcript.String()))
	}
	// A job that disappears ends the follow.
	delete(f.reads, "api")
	if next := m.onFollowTick("api"); next != nil || m.following != "" {
		t.Error("follow continued after the job vanished")
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "job api is gone") {
		t.Errorf("vanished job:\n%s", stripANSI(m.transcript.String()))
	}
}

func TestEscStopsFollowingBeforeInterrupting(t *testing.T) {
	m := slashModel(t)
	m.sess.Jobs = logJobs("")
	m.following = "api"
	cancelled := false
	m.turnCancel = func() { cancelled = true }
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.following != "" || cancelled {
		t.Errorf("Esc: following=%q cancelled=%v", m.following, cancelled)
	}
	m.handleSlash("/logs stop")
	if !strings.Contains(shown(m), "not following anything") {
		t.Errorf("/logs stop:\n%s", shown(m))
	}
}

func TestLogsErrorsPromotesFailuresToTheModel(t *testing.T) {
	m := slashModel(t)
	sr := &scriptedRun{}
	m.run = sr.run
	m.sess.Jobs = logJobs("GET / 200\npanic: nil map\n\tmain.go:12\nGET / 200\n")

	m.state = stateRunning
	m.handleSlash("/logs --errors api")
	if !strings.Contains(shown(m), "/logs --errors isn't available") {
		t.Errorf("mid-turn --errors:\n%s", shown(m))
	}

	m.state = stateIdle
	_, cmd := m.handleSlash("/logs --errors api")
	if cmd == nil {
		t.Fatal("error lines were not sent to the model")
	}
	awaitMsg(t, m.events)
	got := sr.last()
	if !strings.Contains(got, "panic: nil map\n\tmain.go:12") || strings.Contains(got, "GET / 200") {
		t.Errorf("prompt should carry only the failure:\n%s", got)
	}
	if !strings.Contains(shown(m), "From api's log (2 error lines)") {
		t.Errorf("promotion header:\n%s", shown(m))
	}
}

func TestLogsErrorsWithACleanLog(t *testing.T) {
	m := jobsModel(t, logJobs("GET / 200\nGET /a 200\n"))
	if _, cmd := m.logsCommand([]string{"-e", "api"}); cmd != nil {
		t.Error("a clean log started a turn")
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "No error-looking lines in api's log") {
		t.Errorf("clean log:\n%s", stripANSI(m.transcript.String()))
	}
}

// brokenJobs is a store whose restarts and kills fail.
type brokenJobs struct {
	*fakeJobs
	restart   tools.JobStatus
	restartOK bool
}

func (b brokenJobs) Restart(string) (tools.JobStatus, bool) { return b.restart, b.restartOK }
func (b brokenJobs) Kill(string) bool                       { return false }

func TestRestartAndStopJobFailures(t *testing.T) {
	idle := &fakeJobs{jobs: []tools.JobStatus{{ID: "bash_1", Name: "a"}, {ID: "bash_2", Name: "b"}}}
	m := jobsModel(t, nil)
	m.sess.Jobs = brokenJobs{fakeJobs: idle}
	m.restartCommand(nil)
	m.stopJobCommand(nil)
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "usage: /restart <job>") || !strings.Contains(out, "usage: /stopjob <job|all>") {
		t.Errorf("no sole job to default to:\n%s", out)
	}

	m.restartCommand([]string{"ghost"})
	m.stopJobCommand([]string{"ghost"})
	if n := strings.Count(stripANSI(m.transcript.String()), "no job ghost"); n != 2 {
		t.Errorf("unknown job reported %d times:\n%s", n, stripANSI(m.transcript.String()))
	}

	m.sess.Jobs = brokenJobs{fakeJobs: idle, restart: tools.JobStatus{ID: "bash_1", Name: "a"}}
	m.restartCommand([]string{"a"})
	if !strings.Contains(stripANSI(m.transcript.String()), "stopped a but it did not come back up — /logs a") {
		t.Errorf("failed restart:\n%s", stripANSI(m.transcript.String()))
	}
}

func TestStopJobByName(t *testing.T) {
	f := &fakeJobs{jobs: []tools.JobStatus{{ID: "bash_1", Name: "api", Running: true}}}
	m := jobsModel(t, f)
	m.stopJobCommand([]string{"api"})
	if len(f.killed) != 1 || f.killed[0] != "api" {
		t.Errorf("killed = %v", f.killed)
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "stopped api") {
		t.Errorf("stop:\n%s", stripANSI(m.transcript.String()))
	}
}

func TestTailLines(t *testing.T) {
	if got := tailLines("a\nb\nc\n", 2); got != "b\nc" {
		t.Errorf("tail 2 = %q", got)
	}
	if got := tailLines("a\nb\n", 0); got != "a\nb" {
		t.Errorf("tail with no height = %q", got)
	}
}

func TestShowLongPagesLongOutputWhenAPagerExists(t *testing.T) {
	fakeBin(t, "more")
	m := jobsModel(t, nil)
	m.height = 2
	long := strings.Repeat("row\n", 20)
	if cmd := m.showLong("search", long); cmd == nil {
		t.Error("long output was not paged")
	}
	if m.transcript.Len() != 0 {
		t.Error("paged output was also printed")
	}
}

func TestShowLongPrintsWhenThereIsNoPager(t *testing.T) {
	fakeBin(t)
	m := jobsModel(t, nil)
	m.height = 2
	if cmd := m.showLong("search", strings.Repeat("row\n", 20)+"the end"); cmd != nil {
		t.Error("returned a pager command with no pager on PATH")
	}
	if !strings.Contains(stripANSI(m.transcript.String()), "the end") {
		t.Errorf("fallback did not print:\n%s", stripANSI(m.transcript.String()))
	}
	if matches, _ := filepath.Glob(filepath.Join(os.Getenv("TMPDIR"), "klaudia-*")); len(matches) != 0 {
		t.Errorf("temp file left behind: %v", matches)
	}
	if errNoPager.Error() != "no pager found on PATH" {
		t.Errorf("errNoPager = %q", errNoPager.Error())
	}
}

func TestOpenInEditor(t *testing.T) {
	fakeBin(t, "vim")
	m := slashModel(t)
	write(t, m.sess.CWD, "pkg/main.go", "package main\n")

	m.handleSlash("/open")
	if !strings.Contains(shown(m), "usage: /open <path[:line[:col]]>") {
		t.Errorf("/open with no path:\n%s", shown(m))
	}
	m.handleSlash("/open nothere.go:3")
	if !strings.Contains(shown(m), "open: no such file: nothere.go:3") {
		t.Errorf("missing file:\n%s", shown(m))
	}
	m.handleSlash("/open pkg/main.go:12:4")
	if !strings.Contains(shown(m), "open: set $EDITOR") {
		t.Errorf("no editor:\n%s", shown(m))
	}
	t.Setenv("EDITOR", "vim")
	_, cmd := m.handleSlash("/open pkg/main.go:12:4")
	if cmd == nil {
		t.Fatal("no editor command")
	}
	if !strings.Contains(shown(m), "Opening pkg/main.go:12:4") {
		t.Errorf("open announcement:\n%s", shown(m))
	}
	if len(m.recentPaths) == 0 || m.recentPaths[0] != "pkg/main.go" {
		t.Errorf("opened file not ranked for completion: %v", m.recentPaths)
	}
}

func TestTrustControllerWrapsARealGate(t *testing.T) {
	if NewTrustController(nil) != nil {
		t.Fatal("no gate should mean no controller")
	}
	home, proj := t.TempDir(), t.TempDir()
	roots := trust.NewRoots(home, proj)
	g := &agent.HostGate{Roots: func() trust.Roots { return roots }, Ledger: trust.NewLedger(roots)}
	tc := NewTrustController(g)

	tc.SetPolicy(agent.HostEnforce)
	if tc.Policy() != agent.HostEnforce {
		t.Errorf("policy = %q", tc.Policy())
	}
	if len(tc.Reports()) != 0 || len(tc.Grants()) != 0 {
		t.Error("a fresh gate has history")
	}
	effect := trust.Effect{Zone: trust.ZoneHost, Kind: trust.KindServiceControl,
		Res: trust.Resource{Class: "service", ID: "nginx"}, Target: trust.LocalTarget(), Certain: true}
	if tc.Covers(nil) || tc.Covers([]trust.Effect{effect}) {
		t.Error("nothing is granted yet, so nothing is covered")
	}
	g1, err := g.Ledger.Mint(trust.Request{Summary: "restart nginx", Services: []string{"nginx"}})
	if err != nil {
		t.Fatal(err)
	}
	if !tc.Covers([]trust.Effect{effect}) {
		t.Error("a granted service control was not covered")
	}
	if len(tc.Grants()) != 1 || !tc.Revoke(g1.ID) {
		t.Fatalf("grants = %v", tc.Grants())
	}
	if _, err := g.Ledger.Mint(trust.Request{Summary: "again", Services: []string{"nginx"}}); err != nil {
		t.Fatal(err)
	}
	if n := tc.RevokeAll(); n != 1 {
		t.Errorf("RevokeAll = %d, want 1", n)
	}

	// Covers must be safe on a gate with no ledger.
	if NewTrustController(&agent.HostGate{}).Covers([]trust.Effect{effect}) {
		t.Error("a gate without a ledger covered an effect")
	}
}

// fakeExec is a sandbox.Executor that returns a canned response.
type fakeExec struct {
	resp sandbox.Response
	err  error
	got  sandbox.Request
}

func (f *fakeExec) Name() string { return "fake" }
func (f *fakeExec) Run(_ context.Context, req sandbox.Request) (sandbox.Response, error) {
	f.got = req
	return f.resp, f.err
}
func (f *fakeExec) Argv(sandbox.Request) (string, []string) { return "sh", nil }

func TestBangRunsThroughTheSessionExecutor(t *testing.T) {
	m := slashModel(t)
	ex := &fakeExec{resp: sandbox.Response{Stdout: "partial", Stderr: "warning: x\n", ExitCode: 3}}
	m.executor = ex
	m.state = stateRunning
	_, cmd := m.runBang("!make build")
	if cmd == nil {
		t.Fatal("no command to run")
	}
	msg := cmd().(bangResultMsg)
	if ex.got.Command != "make build" || ex.got.WorkingDir != m.sess.CWD {
		t.Errorf("executor request = %+v", ex.got)
	}
	if msg.output != "partial\nwarning: x\n" {
		t.Errorf("stdout and stderr were not joined cleanly: %q", msg.output)
	}
	m.Update(msg)
	out := shown(m)
	if !strings.Contains(out, "partial") || !strings.Contains(out, "[exit 3]") {
		t.Errorf("result:\n%s", out)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v after the command finished", m.state)
	}
	if ctx := m.takeShellContext(); !strings.Contains(ctx, "$ make build") || !strings.Contains(ctx, "(exit 3)") {
		t.Errorf("shell context:\n%s", ctx)
	}
}

func TestBangLongOutputPointsAtTheFullResult(t *testing.T) {
	m := slashModel(t)
	m.onBangResult(bangResultMsg{command: "seq 100", output: strings.Repeat("n\n", 100)})
	out := shown(m)
	if !strings.Contains(out, "more lines · /last 1 for full output") {
		t.Errorf("clipped output:\n%s", out)
	}
	if res, ok := m.results.latest(); !ok || strings.Count(res.content, "\n") != 99 {
		t.Errorf("the full output was not kept for /last")
	}
}

func TestBangReportsAFailureToRun(t *testing.T) {
	m := slashModel(t)
	m.onBangResult(bangResultMsg{command: "x", err: errors.New("executor unavailable")})
	out := shown(m)
	if !strings.Contains(out, "executor unavailable") || strings.Contains(out, "[exit") {
		t.Errorf("run failure:\n%s", out)
	}
	m.onBangResult(bangResultMsg{command: "true"})
	if !strings.Contains(shown(m), "(no output ·") {
		t.Errorf("silent success:\n%s", shown(m))
	}
}

func TestEmptyBangExplainsItself(t *testing.T) {
	m := slashModel(t)
	if _, cmd := m.runBang("!  "); cmd != nil {
		t.Error("an empty ! ran something")
	}
	if !strings.Contains(shown(m), "! runs a shell command directly") {
		t.Errorf("empty bang:\n%s", shown(m))
	}
}

func TestExportedThemeHelpers(t *testing.T) {
	if !IsTheme("Nord") || !IsTheme("tokyo-night") || IsTheme("neon-disco") {
		t.Error("IsTheme disagrees with the theme list")
	}
	names := ThemeNames()
	for _, th := range renderThemes {
		if !strings.Contains(names, th.id) {
			t.Errorf("ThemeNames %q is missing %q", names, th.id)
		}
	}
}
