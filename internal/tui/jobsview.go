package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Logs get the terminal's scrollback and the user's own pager, not a viewport.
//
// §12 asks for independent scrollback, Page Up/Down, search, selection-copy,
// follow mode, and — pointedly — "incoming logs don't snap the viewport back
// down". Every one of those is something `less` and the terminal already do
// well, and the last one is only ever a problem *because* an app owns a
// viewport. Phase 1 deleted ours for exactly this reason. Rebuilding it here to
// satisfy a checklist would reintroduce the bug the checklist is about.
//
// So: `/logs <job>` hands the log file to $PAGER, and `/logs -f` prints into
// real scrollback. Scrolling up during follow cannot be snapped back down,
// because nothing is managing the region you scrolled.

// JobController lets the TUI drive jobs without owning the store.
type JobController interface {
	List() []tools.JobStatus
	// OnExit registers the callback fired when any job finishes. The TUI
	// installs its own so a crash is reported the moment it happens rather
	// than whenever someone next looks.
	OnExit(func(tools.JobStatus))
	Log(ref string) (text, path string, ok bool)
	Read(ref string) (tools.ShellOutput, bool)
	Kill(ref string) bool
	Restart(ref string) (tools.JobStatus, bool)
}

// jobExitMsg reports that a job finished, so the user hears about a crash
// without having to ask.
type jobExitMsg struct{ status tools.JobStatus }

// followTickMsg drives a follow-mode poll.
type followTickMsg struct{ ref string }

// jobsCommand handles /jobs.
func (m *Model) jobsCommand() {
	if m.sess.Jobs == nil {
		m.appendLine(errStyle.Render("background jobs are not available in this session"))
		return
	}
	m.appendLine(bannerStyle.Render(tools.RenderJobs(m.sess.Jobs.List())))
}

// logsCommand handles /logs [-f] [--errors] <job>.
func (m *Model) logsCommand(args []string) (tea.Model, tea.Cmd) {
	if m.sess.Jobs == nil {
		m.appendLine(errStyle.Render("background jobs are not available in this session"))
		return m, nil
	}
	var follow, errorsOnly bool
	var ref string
	for _, a := range args {
		switch a {
		case "-f", "--follow":
			follow = true
		case "--errors", "-e":
			errorsOnly = true
		default:
			ref = a
		}
	}
	if ref == "" {
		// One job is the common case; naming it every time would be noise.
		running := runningJobs(m.sess.Jobs.List())
		if len(running) == 1 {
			ref = running[0].Name
		} else {
			m.appendLine(bannerStyle.Render(tools.RenderJobs(m.sess.Jobs.List())))
			m.appendLine(hintStyle.Render("usage: /logs [-f] [--errors] <job>"))
			return m, nil
		}
	}

	text, path, ok := m.sess.Jobs.Log(ref)
	if !ok {
		m.appendLine(errStyle.Render("no job " + ref))
		return m, nil
	}

	if errorsOnly {
		if m.busyGuard("/logs --errors") {
			return m, nil
		}
		return m, m.promoteErrors(ref, text)
	}
	if follow {
		return m, m.startFollow(ref)
	}

	if strings.TrimSpace(text) == "" {
		m.appendLine(bannerStyle.Render(ref + " has produced no output yet."))
		return m, nil
	}
	// Hand the pager the real file when there is one: less can then follow it
	// with F, and the user keeps whatever they configured.
	if path != "" {
		if cmd, ok := pagerCommand(os.Getenv, path); ok {
			return m, tea.ExecProcess(cmd, func(error) tea.Msg { return pagerDoneMsg{} })
		}
	}
	cmd, err := m.pageText("logs-"+ref, text)
	if err != nil {
		// No pager at all: print the tail rather than nothing.
		m.appendLine(bannerStyle.Render(tailLines(text, m.height)))
		return m, nil
	}
	return m, cmd
}

// promoteErrors pulls just the failure lines into the conversation.
//
// §12's last ask: an important failure should reach the model without dumping
// four thousand lines of request logging into the context window.
func (m *Model) promoteErrors(ref, text string) tea.Cmd {
	lines := errorLines(text)
	if len(lines) == 0 {
		m.appendLine(bannerStyle.Render("No error-looking lines in " + ref + "'s log."))
		return nil
	}
	body := strings.Join(lines, "\n")
	m.appendLine(errStyle.Render(fmt.Sprintf("From %s's log (%d error lines):", ref, len(lines))))
	m.appendLine(toolStyle.Render("  " + strings.ReplaceAll(body, "\n", "\n  ")))
	return m.startTurn(fmt.Sprintf(
		"Errors from the log of job %q:\n\n```\n%s\n```\n\nWhat is going wrong?", ref, body))
}

// errorPatterns are the shapes a failure takes in a server log. Deliberately
// broad: a missed line costs a follow-up, a false positive costs one extra line
// of context.
var errorPatterns = []string{
	"error", "err:", "fatal", "panic", "exception", "traceback",
	"failed", "failure", "cannot ", "could not", "refused",
	"eaddrinuse", "econnrefused", "enoent", "segmentation fault",
	" 500 ", " 502 ", " 503 ",
}

// errorLines keeps failure-looking lines plus the indented lines under them, so
// a stack trace arrives whole rather than as its first line.
func errorLines(text string) []string {
	var out []string
	inTrace := false
	for _, ln := range strings.Split(text, "\n") {
		low := strings.ToLower(ln)
		hit := false
		for _, p := range errorPatterns {
			if strings.Contains(low, p) {
				hit = true
				break
			}
		}
		switch {
		case hit:
			out = append(out, ln)
			inTrace = true
		case inTrace && (strings.HasPrefix(ln, "\t") || strings.HasPrefix(ln, "  ") || strings.HasPrefix(ln, "    at ")):
			out = append(out, ln)
		default:
			inTrace = false
		}
		if len(out) > 200 {
			out = append(out, "… (truncated)")
			return out
		}
	}
	return out
}

// startFollow begins tailing a job into scrollback.
func (m *Model) startFollow(ref string) tea.Cmd {
	m.following = ref
	m.appendLine(bannerStyle.Render("Following " + ref +
		" — scroll freely, this prints into your terminal's scrollback. Esc or /logs stop to stop."))
	// Print what is already there so the user has context, then stream.
	if out, ok := m.sess.Jobs.Read(ref); ok && out.Output != "" {
		m.appendLine(toolStyle.Render(strings.TrimRight(out.Output, "\n")))
	}
	return followTick(ref)
}

func followTick(ref string) tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return followTickMsg{ref: ref} })
}

// onFollowTick prints whatever the followed job produced since the last tick.
func (m *Model) onFollowTick(ref string) tea.Cmd {
	if m.following != ref || m.sess.Jobs == nil {
		return nil // follow was stopped, or switched to another job
	}
	out, ok := m.sess.Jobs.Read(ref)
	if !ok {
		m.appendLine(errStyle.Render("job " + ref + " is gone; stopping follow"))
		m.following = ""
		return nil
	}
	if s := strings.TrimRight(out.Output, "\n"); s != "" {
		m.appendLine(toolStyle.Render(s))
	}
	if !out.Running {
		m.appendLine(bannerStyle.Render(fmt.Sprintf("%s exited (%d); stopping follow.", ref, out.ExitCode)))
		m.following = ""
		return nil
	}
	return followTick(ref)
}

// stopFollow ends follow mode without touching the job.
func (m *Model) stopFollow() bool {
	if m.following == "" {
		return false
	}
	m.appendLine(bannerStyle.Render("Stopped following " + m.following + " (it is still running)."))
	m.following = ""
	return true
}

// onJobExit reports a job finishing.
//
// A dev server that dies at 14:02 and is noticed at 14:40 costs the intervening
// half hour. The line is printed whether or not anyone was looking at the job.
func (m *Model) onJobExit(st tools.JobStatus) {
	if st.ExitCode == 0 {
		m.appendLine(bannerStyle.Render(fmt.Sprintf("job %s exited cleanly", st.Name)))
		return
	}
	m.appendLine(errStyle.Render(fmt.Sprintf(
		"job %s exited with code %d — /logs %s", st.Name, st.ExitCode, st.Name)))
}

// restartCommand handles /restart <job>.
func (m *Model) restartCommand(args []string) {
	if m.sess.Jobs == nil {
		m.appendLine(errStyle.Render("background jobs are not available in this session"))
		return
	}
	ref := firstOr(args, soleRunningJob(m.sess.Jobs))
	if ref == "" {
		m.appendLine(errStyle.Render("usage: /restart <job>"))
		return
	}
	st, ok := m.sess.Jobs.Restart(ref)
	if st.ID == "" {
		m.appendLine(errStyle.Render("no job " + ref))
		return
	}
	if !ok {
		m.appendLine(errStyle.Render("stopped " + st.Name + " but it did not come back up — /logs " + st.Name))
		return
	}
	m.appendLine(bannerStyle.Render(fmt.Sprintf("restarted %s (%s)", st.Name, st.ID)))
}

// stopJobCommand handles /stopjob <job|all>.
func (m *Model) stopJobCommand(args []string) {
	if m.sess.Jobs == nil {
		m.appendLine(errStyle.Render("background jobs are not available in this session"))
		return
	}
	if len(args) > 0 && strings.EqualFold(args[0], "all") {
		n := 0
		for _, j := range m.sess.Jobs.List() {
			if j.Running && m.sess.Jobs.Kill(j.ID) {
				n++
			}
		}
		m.appendLine(bannerStyle.Render(fmt.Sprintf("stopped %d job(s)", n)))
		return
	}
	ref := firstOr(args, soleRunningJob(m.sess.Jobs))
	if ref == "" {
		m.appendLine(errStyle.Render("usage: /stopjob <job|all>"))
		return
	}
	if !m.sess.Jobs.Kill(ref) {
		m.appendLine(errStyle.Render("no job " + ref))
		return
	}
	m.appendLine(bannerStyle.Render("stopped " + ref))
}

func runningJobs(jobs []tools.JobStatus) []tools.JobStatus {
	var out []tools.JobStatus
	for _, j := range jobs {
		if j.Running {
			out = append(out, j)
		}
	}
	return out
}

// soleRunningJob returns the only running job's name, or "" when there is a
// choice to make.
func soleRunningJob(c JobController) string {
	if running := runningJobs(c.List()); len(running) == 1 {
		return running[0].Name
	}
	return ""
}

func firstOr(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}

// tailLines returns the last n lines, for the no-pager fallback.
func tailLines(text string, n int) string {
	if n <= 0 {
		n = 40
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
