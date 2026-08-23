package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// A long-running command is a job, not a blocked turn.
//
// `npm run dev` has no natural end. Run in the foreground it holds the agent
// until the timeout and then reports failure; started and forgotten it becomes
// an untracked process that owns port 3000 for the rest of the afternoon. What
// the user actually wants is the thing the spec asks for: start it, keep
// working, look at its logs, restart it by name, and have it go away cleanly.
//
// Everything here is that. Jobs keep the `bash_N` ids the model already uses so
// nothing it learned stops working, and gain a name the user can say out loud.

// jobLogRetention is how long a session's job logs survive. Long enough to read
// the logs of a session you closed this morning, short enough not to accumulate.
const jobLogRetention = 7 * 24 * time.Hour

// Job is one managed background process.
type Job struct {
	ID      string // bash_1 — stable, and what the model refers to
	Name    string // dev, api — what a person refers to
	Command string
	Dir     string
	Target  trust.Target // where it runs: this machine, or a host the task named
	Started time.Time

	proc     *sandbox.BackgroundProcess
	log      *jobLog
	offset   int64 // BashOutput read cursor
	port     string
	portDone bool
	exited   bool
	exitCode int
	ended    time.Time
	restarts int
}

// JobStatus is a snapshot for display.
type JobStatus struct {
	ID       string
	Name     string
	Command  string
	Running  bool
	ExitCode int
	Port     string
	Where    string // "local", or the host/container the job runs on
	Started  time.Time
	Ended    time.Time
	Restarts int
	LogPath  string
	LogBytes int64
}

// JobStore holds the background jobs started by the Bash tool, shared with the
// BashOutput, KillShell, Jobs and RestartJob tools. Session-scoped and safe for
// concurrent use.
type JobStore struct {
	parent   context.Context
	executor sandbox.Executor
	logDir   string

	mu     sync.Mutex
	jobs   map[string]*Job
	order  []string // insertion order, so listings are stable
	seq    int
	names  map[string]int
	onExit func(JobStatus)
}

// NewJobStore creates a store whose jobs are parented to ctx (cancelling it, or
// calling KillAll, terminates them). session names the log directory.
func NewJobStore(ctx context.Context, session string) *JobStore {
	if ctx == nil {
		ctx = context.Background()
	}
	dir := jobLogDir(session)
	go pruneJobLogs(filepath.Dir(dir), jobLogRetention)
	return &JobStore{
		parent: ctx, logDir: dir,
		jobs: map[string]*Job{}, names: map[string]int{},
	}
}

// OnExit registers a callback fired when any job finishes. The frontend uses it
// to say so; without it a crashed dev server stays "running" until something
// happens to read it.
func (s *JobStore) OnExit(fn func(JobStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onExit = fn
}

// StartResult reports what Start did.
type StartResult struct {
	Job       JobStatus
	Duplicate bool // an identical command was already running; this is that job
}

// Start launches req as a managed job.
//
// An identical command already running returns that job instead of a second
// copy. This is the spec's "restart the API operates on the existing API
// process rather than launch a mystery second copy": two dev servers fighting
// over one port is a confusing failure, and the second one usually loses in a
// way that looks like the code is broken.
func (s *JobStore) Start(e sandbox.Executor, req sandbox.Request) (StartResult, error) {
	if e == nil {
		e = s.executor
	}
	s.mu.Lock()
	s.executor = e
	for _, id := range s.order {
		j := s.jobs[id]
		if !j.exited && j.Command == req.Command && j.Dir == req.WorkingDir {
			st := j.status()
			s.mu.Unlock()
			return StartResult{Job: st, Duplicate: true}, nil
		}
	}
	s.seq++
	id := fmt.Sprintf("bash_%d", s.seq)
	name := s.uniqueNameLocked(jobName(req.Command))
	j := &Job{
		ID: id, Name: name, Command: req.Command, Dir: req.WorkingDir,
		Target: jobTarget(req.Command), Started: time.Now(),
		log: newJobLog(s.logDir, name),
	}
	s.jobs[id] = j
	s.order = append(s.order, id)
	s.mu.Unlock()

	if err := s.launch(j, req); err != nil {
		s.mu.Lock()
		delete(s.jobs, id)
		s.order = s.order[:len(s.order)-1]
		s.mu.Unlock()
		j.log.Close()
		return StartResult{}, err
	}
	return StartResult{Job: j.status()}, nil
}

// launch starts (or relaunches) a job's process.
func (s *JobStore) launch(j *Job, req sandbox.Request) error {
	req.Timeout = 0 // a job runs until it exits or is stopped
	proc, err := sandbox.StartBackgroundWith(s.parent, s.executor, req, sandbox.BackgroundOptions{
		Sink:   j.log,
		OnExit: func(code int) { s.jobExited(j.ID, code) },
	})
	if err != nil {
		return err
	}
	s.mu.Lock()
	j.proc = proc
	j.exited = false
	j.exitCode = 0
	j.ended = time.Time{}
	s.mu.Unlock()
	return nil
}

func (s *JobStore) jobExited(id string, code int) {
	s.mu.Lock()
	j, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return
	}
	j.exited = true
	j.exitCode = code
	j.ended = time.Now()
	st, fn := j.status(), s.onExit
	s.mu.Unlock()
	if fn != nil {
		fn(st)
	}
}

// ShellOutput is the incremental read of a job's output.
type ShellOutput struct {
	ID       string
	Name     string
	Command  string
	Output   string
	Running  bool
	ExitCode int
}

// Read returns output produced since the last read, advancing the cursor.
func (s *JobStore) Read(ref string) (ShellOutput, bool) {
	s.mu.Lock()
	j := s.lookupLocked(ref)
	if j == nil {
		s.mu.Unlock()
		return ShellOutput{}, false
	}
	offset, log := j.offset, j.log
	s.mu.Unlock()

	data, newOffset := log.ReadFrom(offset)

	s.mu.Lock()
	j.offset = newOffset
	s.notePortLocked(j, data)
	out := ShellOutput{
		ID: j.ID, Name: j.Name, Command: j.Command, Output: data,
		Running: !j.exited, ExitCode: j.exitCode,
	}
	s.mu.Unlock()
	return out, true
}

// Log returns a job's whole log and its file path.
func (s *JobStore) Log(ref string) (text, path string, ok bool) {
	s.mu.Lock()
	j := s.lookupLocked(ref)
	s.mu.Unlock()
	if j == nil {
		return "", "", false
	}
	all, _ := j.log.ReadFrom(0)
	return all, j.log.Path(), true
}

// Kill terminates a job. Returns false if there is no such job.
func (s *JobStore) Kill(ref string) bool {
	s.mu.Lock()
	j := s.lookupLocked(ref)
	s.mu.Unlock()
	if j == nil {
		return false
	}
	j.proc.Kill()
	return true
}

// Restart stops a job and starts the same command again in the same slot,
// keeping its id, name and log. This is what makes "restart the API" act on the
// API rather than produce a second one.
func (s *JobStore) Restart(ref string) (JobStatus, bool) {
	s.mu.Lock()
	j := s.lookupLocked(ref)
	if j == nil {
		s.mu.Unlock()
		return JobStatus{}, false
	}
	req := sandbox.Request{Command: j.Command, WorkingDir: j.Dir}
	proc := j.proc
	s.mu.Unlock()

	proc.Kill()
	// Wait for the old process to actually go before starting a replacement,
	// or the new one binds a port the old one still holds and dies immediately.
	deadline := time.Now().Add(10 * time.Second)
	for proc.Running() && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}

	s.mu.Lock()
	j.restarts++
	j.offset = j.log.Size() // the pre-restart log stays on disk; reads resume after it
	j.port, j.portDone = "", false
	j.Started = time.Now()
	s.mu.Unlock()
	j.log.note("restarted (%s)", time.Now().Format(time.Kitchen))

	if err := s.launch(j, req); err != nil {
		s.mu.Lock()
		j.exited, j.exitCode = true, -1
		st := j.status()
		s.mu.Unlock()
		return st, false
	}
	return j.status(), true
}

// List returns every job, oldest first.
func (s *JobStore) List() []JobStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobStatus, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.jobs[id].status())
	}
	return out
}

// KillAll terminates every job (session teardown).
func (s *JobStore) KillAll() {
	s.mu.Lock()
	jobs := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	s.mu.Unlock()
	for _, j := range jobs {
		j.proc.Kill()
		j.log.Close()
	}
}

// lookupLocked resolves a job by id or by name. Callers say "api" far more
// often than "bash_2", and requiring the id would make the names decorative.
func (s *JobStore) lookupLocked(ref string) *Job {
	if j, ok := s.jobs[ref]; ok {
		return j
	}
	for _, id := range s.order {
		if strings.EqualFold(s.jobs[id].Name, ref) {
			return s.jobs[id]
		}
	}
	return nil
}

func (s *JobStore) uniqueNameLocked(base string) string {
	s.names[base]++
	if n := s.names[base]; n > 1 {
		return fmt.Sprintf("%s-%d", base, n)
	}
	return base
}

func (j *Job) status() JobStatus {
	return JobStatus{
		ID: j.ID, Name: j.Name, Command: j.Command,
		Running: !j.exited, ExitCode: j.exitCode, Port: j.port,
		Where: jobWhere(j.Target), Started: j.Started, Ended: j.ended,
		Restarts: j.restarts, LogPath: j.log.Path(), LogBytes: j.log.Size(),
	}
}

func jobWhere(t trust.Target) string {
	if t.Local || t.Host == "" {
		return "local"
	}
	if t.Via != "" {
		return t.Via + ":" + t.Host
	}
	return t.Host
}

// jobTarget reuses the trust classifier's remote detection so a job started
// over ssh or in a container is labelled as running there. §11 asks for
// local-vs-remote to be clear, and the machinery already exists.
func jobTarget(command string) trust.Target {
	as := trust.ClassifyCommand(command, trust.Roots{})
	for _, t := range as.Targets {
		if t.Local {
			continue
		}
		// The classifier calls a container "remote" because that is the right
		// answer for the *trust* question — a write inside a container is not a
		// write to this machine. For a job the question is different: where is
		// the work happening, so the user knows whose logs these are. A plain
		// `docker compose up` runs on this machine's daemon and is local;
		// `docker -H tcp://build:2375` is not, and the classifier already
		// distinguishes them by filling in the real host.
		if t.Host == "container" {
			return trust.LocalTarget()
		}
		return t
	}
	return trust.LocalTarget()
}

// notePortLocked records the first port a job mentions.
func (s *JobStore) notePortLocked(j *Job, output string) {
	if j.portDone || output == "" {
		return
	}
	if p := detectPort(output); p != "" {
		j.port = p
		j.portDone = true
	}
}

// portPatterns find the port a server announces. Best effort by construction —
// a server that prints nothing recognisable simply has no port shown, which is
// better than showing a number scraped out of a timestamp.
var portPatterns = []*regexp.Regexp{
	regexp.MustCompile(`https?://[^\s/]*:(\d{2,5})`),
	regexp.MustCompile(`(?i)(?:listening|running|started|serving|ready)\b[^\n]{0,40}?:(\d{2,5})\b`),
	regexp.MustCompile(`(?i)\bport[\s:=]+(\d{2,5})\b`),
}

func detectPort(output string) string {
	for _, re := range portPatterns {
		if m := re.FindStringSubmatch(output); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

// jobName derives a short, sayable name from a command line.
//
// Derived rather than mapped: `npm run dev` becomes "dev" and `make dev-api`
// becomes "dev-api", which is what the user already calls them. A lookup table
// turning "npm run dev" into "web" would be guessing at their vocabulary.
func jobName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "job"
	}
	prog := filepath.Base(fields[0])
	rest := fields[1:]

	next := func(i int) string {
		if i < len(rest) {
			return rest[i]
		}
		return ""
	}
	switch prog {
	case "npm", "pnpm", "yarn", "bun", "deno":
		// npm run dev → dev; yarn dev → dev
		if next(0) == "run" || next(0) == "run-script" {
			if n := next(1); n != "" {
				return sanitiseName(n)
			}
		}
		if n := next(0); n != "" && !strings.HasPrefix(n, "-") {
			return sanitiseName(n)
		}
	case "make", "just", "task":
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				return sanitiseName(a)
			}
		}
	case "docker", "podman", "nerdctl":
		if next(0) == "compose" {
			return "compose"
		}
	case "go":
		if next(0) == "run" {
			for _, a := range rest[1:] {
				if !strings.HasPrefix(a, "-") {
					return sanitiseName(filepath.Base(strings.TrimSuffix(a, ".go")))
				}
			}
			return "go-run"
		}
	case "cargo", "mvn", "gradle", "./gradlew":
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				return sanitiseName(a)
			}
		}
	case "python", "python3":
		if next(0) == "-m" && next(1) != "" {
			return sanitiseName(next(1))
		}
	}
	return sanitiseName(prog)
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitiseName(s string) string {
	s = unsafeName.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "job"
	}
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}

// RenderJobs formats the job table for /jobs.
func RenderJobs(jobs []JobStatus) string {
	if len(jobs) == 0 {
		return "No jobs. Start one with a background command (Bash run_in_background)."
	}
	var b strings.Builder
	b.WriteString("Jobs\n")
	for _, j := range jobs {
		state := "running"
		switch {
		case !j.Running && j.ExitCode == 0:
			state = "exited"
		case !j.Running:
			state = fmt.Sprintf("crashed (%d)", j.ExitCode)
		}
		extra := ""
		if j.Port != "" {
			extra = "  :" + j.Port
		}
		if j.Where != "local" {
			extra += "  @" + j.Where
		}
		if j.Restarts > 0 {
			extra += fmt.Sprintf("  ×%d restarts", j.Restarts)
		}
		fmt.Fprintf(&b, "  %-8s %-8s %-30s %-14s%s\n",
			j.ID, j.Name, oneLineCommand(j.Command, 30), state, extra)
	}
	b.WriteString("  /logs <name> · /logs -f <name> · /restart <name> · /stopjob <name>")
	return b.String()
}

// fmtShortDuration renders an age in the least noisy unit.
func fmtShortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	}
}

func oneLineCommand(cmd string, width int) string {
	cmd = strings.Join(strings.Fields(cmd), " ")
	if len(cmd) > width {
		return cmd[:width-1] + "…"
	}
	return cmd
}

// sortJobsByName is used where a stable alphabetical order reads better than
// start order (completions).
func sortJobsByName(jobs []JobStatus) {
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
}

// unknownJobMsg names the jobs that do exist. A bare "no such job" makes the
// model guess again with another id it invented; listing the real ones lets it
// correct itself in one step.
func (s *JobStore) unknownJobMsg(ref string) string {
	jobs := s.List()
	if len(jobs) == 0 {
		return fmt.Sprintf("No job %q, and none are running.", ref)
	}
	names := make([]string, 0, len(jobs))
	for _, j := range jobs {
		state := "running"
		if !j.Running {
			state = "exited"
		}
		names = append(names, fmt.Sprintf("%s (%s, %s)", j.Name, j.ID, state))
	}
	return fmt.Sprintf("No job %q. Current jobs: %s.", ref, strings.Join(names, "; "))
}
