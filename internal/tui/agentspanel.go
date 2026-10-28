package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// The sub-agent panel: every sub-agent still working, under the prompt, the
// way Claude Code lists them.
//
//	⏺ main
//	  ◯ Explore  Read internal/tui/view.go                 1m 02s · 12.3k tokens
//	  ◯ general-purpose  Bash go test ./...                   48s · 8.1k tokens
//
// A sub-agent's steps used to go to the scrollback one "↳" line each, so
// sub-agents running side by side interleaved there, and nothing said what
// any of them had cost. The steps are live state, so they belong here; what a
// sub-agent's run came to (its worktree adopted, its result) still goes to
// the scrollback. Background sub-agents are listed here too, rather than
// among the Bash jobs.

// agentsPanelRows caps the list.
const agentsPanelRows = 8

// liveAgent is a foreground Agent call in flight.
type liveAgent struct {
	kind, desc, last string
	started          time.Time
	tokens           int64
}

// noteAgentStart records a foreground Agent call. A background one returns at
// once and is listed from the runner instead.
func (m *Model) noteAgentStart(ev agent.Event) {
	if ev.ToolName != "Agent" {
		return
	}
	f := toolFields(ev.Input)
	if f["run_in_background"] == "true" {
		return
	}
	if m.liveAgents == nil {
		m.liveAgents = map[string]*liveAgent{}
	}
	m.liveAgents[ev.ToolUseID] = &liveAgent{kind: f["subagent_type"], desc: f["description"], started: time.Now()}
}

// noteAgentProgress folds a tool_progress event into the panel and reports
// whether the panel has taken it, so it is not also printed. A sub-agent's
// steps are taken; the lines the spawner indents (its checkout, what was
// applied) are outcomes and still printed.
func (m *Model) noteAgentProgress(ev agent.Event) bool {
	a := m.liveAgents[ev.ToolUseID]
	if a == nil {
		return false
	}
	if ev.Tokens > 0 {
		a.tokens = ev.Tokens
	}
	if ev.Text == "" {
		return true
	}
	if strings.HasPrefix(ev.Text, "  ") {
		return false
	}
	a.last = ev.Text
	return true
}

// agentRow is one line of the panel before layout.
type agentRow struct {
	started     time.Time
	kind, doing string
	tokens      int64
}

// agentsPanel renders the running sub-agents, oldest first, or "" when none.
func (m *Model) agentsPanel() string {
	var rows []agentRow
	for _, a := range m.liveAgents {
		rows = append(rows, agentRow{a.started, a.kind, firstNonBlank(a.last, a.desc), a.tokens})
	}
	for _, b := range m.runningAgents() {
		kind, desc, _ := strings.Cut(b.Label, ": ")
		rows = append(rows, agentRow{b.Started, kind + " (" + b.ID + ")", firstNonBlank(b.Last, desc), b.Tokens})
	}
	if len(rows) == 0 {
		return ""
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].started.Before(rows[j].started) })
	// caption indents by two, and a line must stay narrower than the
	// terminal (the inline renderer's rule), so the row gets width-3.
	width := max(m.width-3, 10)
	lines := []string{caption(toolStyle.Render("⏺ main"))}
	for i, r := range rows {
		if i == agentsPanelRows {
			lines = append(lines, caption(hintStyle.Render(fmt.Sprintf("  +%d more", len(rows)-agentsPanelRows))))
			break
		}
		stats := elapsed(time.Since(r.started))
		if r.tokens > 0 {
			stats += " · " + humanTokens(r.tokens) + " tokens"
		}
		head := "  ◯ " + r.kind + "  "
		room := width - ansi.StringWidth(head) - ansi.StringWidth(stats) - 2
		doing := oneline(r.doing, 200)
		if room < 8 {
			doing = ""
		} else {
			doing = ansi.Truncate(doing, room, "…")
		}
		gap := max(2, width-ansi.StringWidth(head)-ansi.StringWidth(doing)-ansi.StringWidth(stats))
		line := toolStyle.Render(head) + doing + strings.Repeat(" ", gap) + hintStyle.Render(stats)
		if ansi.StringWidth(line) > width { // a terminal too narrow for type and stats
			line = ansi.Truncate(line, width, "…")
		}
		lines = append(lines, caption(line))
	}
	return strings.Join(lines, "\n")
}

func firstNonBlank(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
