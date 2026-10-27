package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
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

// jobsPanel renders running jobs and background agents, oldest first, or ""
// when nothing is running.
func (m *Model) jobsPanel() string {
	type row struct {
		started    time.Time
		head, tail string
	}
	var rows []row
	if m.sess != nil && m.sess.Jobs != nil {
		for _, j := range runningJobs(m.sess.Jobs.List()) {
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
			// Quiet is worth seeing on a long job — a provisioning step that
			// has said nothing for twenty minutes may be fine or may be stuck,
			// and only the person watching can tell. Reported, not judged.
			if !j.LastOutput.IsZero() && time.Since(j.LastOutput) >= time.Minute {
				head += " · quiet " + elapsed(time.Since(j.LastOutput))
			}
			rows = append(rows, row{j.Started, head, oneline(j.Command, 200)})
		}
	}
	for _, a := range m.runningAgents() {
		tail := a.Label
		if a.Last != "" {
			tail += " — " + a.Last
		}
		head := "⏵ " + a.ID + " · " + elapsed(time.Since(a.Started))
		if q := time.Since(a.LastAt); !a.LastAt.IsZero() && q >= time.Minute {
			head += " · quiet " + elapsed(q) // as for jobs: reported, not judged
		}
		rows = append(rows, row{a.Started, head, oneline(tail, 200)})
	}
	if len(rows) == 0 {
		return ""
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].started.Before(rows[j].started) })
	limit := max(m.width-4, 20)
	var lines []string
	for i, r := range rows {
		if i == jobsPanelRows {
			lines = append(lines, caption(hintStyle.Render(fmt.Sprintf("  +%d more · /jobs", len(rows)-jobsPanelRows))))
			break
		}
		tail := ""
		if room := limit - ansi.StringWidth(r.head) - 2; room > 10 {
			tail = "  " + ansi.Truncate(r.tail, room, "…")
		}
		lines = append(lines, caption(toolStyle.Render(ansi.Truncate(r.head, limit, "…"))+hintStyle.Render(tail)))
	}
	return strings.Join(lines, "\n")
}

// runningWorkSummary names what quitting would stop — running jobs with their
// age, then background agents — or "" when nothing is running.
func (m *Model) runningWorkSummary() string {
	var parts []string
	if m.sess != nil && m.sess.Jobs != nil {
		for _, j := range runningJobs(m.sess.Jobs.List()) {
			name := j.Name
			if name == "" {
				name = j.ID
			}
			parts = append(parts, fmt.Sprintf("%s (%s)", name, elapsed(time.Since(j.Started))))
		}
	}
	for _, a := range m.runningAgents() {
		parts = append(parts, a.ID)
	}
	return shortList(parts, 3)
}

// jobsTickMsg redraws the panel so its elapsed times move while idle, when
// nothing else would trigger a frame.
type jobsTickMsg struct{}

// jobsTick schedules the next redraw while a job is running, and lets the
// tick lapse once none is — so an idle session with no jobs costs nothing.
func (m *Model) jobsTick() tea.Cmd {
	if m.jobsTicking || m.runningJobCount()+len(m.runningAgents()) == 0 {
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
