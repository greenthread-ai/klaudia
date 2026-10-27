package tui

import (
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// The running-work panel: what is still going, under the prompt.
//
// The status line said "2 jobs" and nothing else, so the question every count
// raises — which two, for how long, on what port — took a /jobs. Claude Code
// lists background work under its prompt; this is that, for Klaudia's managed
// jobs, and the place background sub-agents will join (roadmap #4).
//
// It is live-region only, like the plan panel: a job list printed into
// scrollback is wrong the moment a job exits.

// jobsPanelRows caps the list; the rest are counted and /jobs shows them all.
const jobsPanelRows = 3

// jobsPanel renders the running jobs, oldest first, or "" when none are.
func (m *Model) jobsPanel() string {
	if m.sess == nil || m.sess.Jobs == nil {
		return ""
	}
	jobs := runningJobs(m.sess.Jobs.List())
	if len(jobs) == 0 {
		return ""
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].Started.Before(jobs[j].Started) })
	limit := max(m.width-4, 20)
	var lines []string
	for i, j := range jobs {
		if i == jobsPanelRows {
			lines = append(lines, caption(hintStyle.Render(fmt.Sprintf("  +%d more · /jobs", len(jobs)-jobsPanelRows))))
			break
		}
		name := j.Name
		if name == "" {
			name = j.ID
		}
		head := "⏵ " + name + " · " + elapsed(time.Since(j.Started))
		if j.Port != "" {
			head += " · :" + j.Port
		}
		if j.Where != "" && j.Where != "local" {
			head += " · " + j.Where
		}
		cmd := ""
		if room := limit - ansi.StringWidth(head) - 2; room > 10 {
			cmd = "  " + ansi.Truncate(oneline(j.Command, room), room, "…")
		}
		lines = append(lines, caption(toolStyle.Render(ansi.Truncate(head, limit, "…"))+hintStyle.Render(cmd)))
	}
	out := lines[0]
	for _, l := range lines[1:] {
		out += "\n" + l
	}
	return out
}

// jobsTickMsg redraws the panel so its elapsed times move while idle, when
// nothing else would trigger a frame.
type jobsTickMsg struct{}

// jobsTick schedules the next redraw while a job is running, and lets the
// tick lapse once none is — so an idle session with no jobs costs nothing.
func (m *Model) jobsTick() tea.Cmd {
	if m.jobsTicking || m.runningJobCount() == 0 {
		return nil
	}
	m.jobsTicking = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return jobsTickMsg{} })
}

// elapsed is a running job's age at whole-second precision ("42s", "3m05s"):
// it ticks once a second, so tenths would only flicker.
func elapsed(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmtDuration(d)
}
