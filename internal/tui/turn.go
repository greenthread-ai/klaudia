package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/goal"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

// hostReportCount is the session's host-gate report count, or 0 when no gate is
// wired up. Used as the completion block's per-turn watermark.
func (m *Model) hostReportCount() int {
	if m.sess == nil || m.sess.Trust == nil {
		return 0
	}
	return len(m.sess.Trust.Reports())
}

// liveMode returns a source for the session's current permission mode, read at
// each check rather than once per turn. Nil when there is no session (tests),
// which leaves the caller's own permission context in place.
func (m *Model) liveMode() func() permission.Mode {
	if m.sess == nil {
		return nil
	}
	sess := m.sess
	return func() permission.Mode { return permission.Mode(sess.PermissionMode) }
}

// startTurn runs the agent in a goroutine, delivering events via the channel,
// and returns the command that drives the spinner + elapsed stopwatch. The turn
// runs under a cancellable context so Esc can interrupt it. A standing /goal is
// re-stated to the model each turn (Ralph-style).
func (m *Model) startTurn(prompt string) tea.Cmd {
	// Belt-and-braces: zero the per-turn live tally so a path that skipped
	// doneMsg can't poison the next reconcile. The normal path also resets
	// these on doneMsg, so this is just a guard.
	m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut = 0, 0, 0
	// Phase tracking: start every turn in "thinking" and mark the clock now
	// so the quiet-detector doesn't fire on a fresh turn that legitimately
	// takes 30s+ to receive its first event (some providers stream slowly).
	m.phase = "thinking"
	m.lastEventAt = time.Now()
	m.activeToolName = ""
	m.activeToolStart = time.Time{}
	// Frame the turn. Goal-setting interviews the user and drafts the spec; an
	// active loop's prompt is already goal.IterationPrompt (built by the caller),
	// so leave it untouched; otherwise re-state any standing /goal (drift guard).
	// Pinned files are re-stated every turn. Mentioned once forty turns ago is,
	// for practical purposes, not in context at all — and compaction is allowed
	// to elide it.
	if pins := m.pinnedBlock(); pins != "" {
		prompt = pins + "\n\n" + prompt
	}

	// A `!` command the user ran since the last turn is context for this one:
	// "revert that" needs a referent. Prepended rather than sent separately so
	// it arrives attached to the instruction that refers to it.
	if shell := m.takeShellContext(); shell != "" {
		prompt = shell + "\n\n" + prompt
	}

	switch {
	case m.goalSetting && m.sess != nil:
		existing, specPath, _ := goal.Read(m.sess.CWD)
		prompt = goal.FacilitatorPrompt(specPath, existing) + "\n\nUser: " + prompt
	case m.loopRemaining > 0 || m.loopWrapUp:
		// prompt is the iteration / wrap-up prompt already; no framing added.
	case m.sess != nil && m.sess.Goal != "":
		prompt = fmt.Sprintf("Standing goal for this session: %s\n\nCurrent instruction: %s", m.sess.Goal, prompt)
	}
	// The completion block describes this turn, not the session — or, in a
	// goal loop, the whole run: only its first turn resets the markers.
	inLoop := m.loopRemaining > 0 || m.loopWrapUp
	if !inLoop || !m.loopSpanOpen {
		m.turnTouched = map[string]bool{}
		m.turnLabel = oneline(prompt, 60)
		m.turnResultsFrom = m.results.seq
		m.turnReportsFrom = m.hostReportCount()
	}
	m.loopSpanOpen = inLoop

	approver := &uiApprover{events: m.events}
	asker := &uiAsker{events: m.events}
	planner := &uiPlanner{events: m.events}
	emit := func(ev agent.Event) { m.events <- eventMsg{ev} }
	ctx, cancel := context.WithCancel(m.ctx)
	m.turnCancel = cancel
	turn := agent.Turn{
		Prompt:     prompt,
		History:    m.history,
		Emit:       emit,
		Approver:   approver,
		Asker:      asker,
		Planner:    planner,
		Interject:  m.steer.drain,
		BeforeEdit: m.beforeEdit,
		// Read live, every permission check: /mode bypass typed mid-turn, or
		// ExitPlanMode being approved, has to reach the very next tool
		// dispatch rather than waiting for the next turn boundary.
		Mode: m.liveMode(),
	}
	go func() {
		res, err := m.run(ctx, turn)
		m.events <- doneMsg{res: res, err: err}
	}()
	return tea.Batch(m.spin.Tick, m.sw.Reset(), m.sw.Start())
}

func (m *Model) renderEvent(ev agent.Event) {
	// Bump the activity clock on every event — bottomView reads this to
	// decide whether to surface the "quiet for…" stuck-state suffix.
	m.lastEventAt = time.Now()
	switch ev.Type {
	case "assistant":
		m.appendText(ev.Text) // streamed deltas (raw until flushed)
		m.phase = "streaming"
	case "tool_use":
		m.flushAssistant() // the assistant message before a tool call is complete
		// Echo the salient input (#1) and, for mutating tools, a change preview (#2).
		m.appendLine(toolStyle.Render("⚙ " + ev.ToolName + toolSummaryWith(ev.ToolName, ev.Input, m.displayPaths)))
		// Remember files Klaudia touches so @-completion can rank them first.
		// Ownership is NOT recorded here: this event is emitted before the tool
		// runs, so stamping the file now would capture its pre-write state and
		// every edit Klaudia made would then look like an edit made by the user.
		// The path is parked until tool_result, when the write has happened —
		// which also means a failed Write never claims the file.
		for _, key := range []string{"file_path", "notebook_path"} {
			p := toolFields(ev.Input)[key]
			m.noteRecentPath(p)
			switch ev.ToolName {
			case "Write", "Edit", "NotebookEdit":
				if p != "" {
					if m.pendingPaths == nil {
						m.pendingPaths = map[string]string{}
					}
					m.pendingPaths[ev.ToolUseID] = p
				}
			}
		}
		if ev.ToolName == "Bash" {
			if m.pendingCommands == nil {
				m.pendingCommands = map[string]string{}
			}
			m.pendingCommands[ev.ToolUseID] = toolFields(ev.Input)["command"]
		}
		if diff := toolDiff(ev.ToolName, ev.Input); diff != "" {
			m.appendLine(diff)
		}
		m.phase = "running " + ev.ToolName
		m.activeToolName = ev.ToolName
		m.activeToolStart = time.Now()
	case "tool_progress":
		// A long-running tool reporting what it is doing — in practice the Agent
		// tool relaying its sub-agent's calls. Indented under the tool line it
		// belongs to, in the same style, because it is the same kind of thing one
		// level down. This goes to scrollback rather than the live region: it is
		// a record of work that happened, not transient state, and it is the only
		// evidence the user gets that a sub-agent is progressing.
		m.flushAssistant()
		m.appendLine(toolStyle.Render("  ↳ " + ev.Text))
		if ev.Text != "" {
			m.phase = "running " + ev.ToolName + " · " + ev.Text
		}
	case "tool_result":
		m.flushAssistant()
		// Store the untruncated output when the tool kept one: /last should show
		// everything the command printed, not the clamped copy the model saw.
		stored := ev.Content
		if ev.FullContent != "" {
			stored = ev.FullContent
		}
		seq := m.results.add(toolResult{
			id: ev.ToolUseID, tool: ev.ToolName, isError: ev.IsError,
			command: m.pendingCommands[ev.ToolUseID],
			at:      time.Now(), content: stored, clamped: ev.FullContent != "",
		})
		delete(m.pendingCommands, ev.ToolUseID)
		// Claim the file now the write has actually happened.
		if p := m.pendingPaths[ev.ToolUseID]; p != "" {
			if !ev.IsError {
				m.noteTouched(p)
			}
			delete(m.pendingPaths, ev.ToolUseID)
		}
		s := m.displayPaths(strings.TrimSpace(ev.Content))
		if s == "" {
			s = "completed"
		}
		style := toolStyle
		prefix := "✓ " + ev.ToolName
		switch {
		case ev.HostBlocked:
			// Not a failure. The call was not allowed and the model will take
			// another route, which is the intended behaviour for the common
			// case — an incidental 2>/dev/null, a scratch file in /tmp. Drawing
			// that in red with a paragraph of policy made Klaudia look broken
			// while it was working exactly as designed.
			style = hintStyle
			prefix = "⊘ " + ev.ToolName
			s = hostBlockedLine(s)
		case ev.IsError:
			style = errStyle
			prefix = "✗ " + ev.ToolName
		case ev.ToolName == "TodoWrite":
			s = todoResultLine(s)
		case ev.ToolName == "ExitPlanMode":
			s = exitPlanResultLine(s)
		}
		// Append the tool's wall-clock duration to its result line so users
		// can see whether a tool was fast or slow without reading timestamps.
		// Matched on ToolName so a misordered event can't suffix the wrong tool.
		if m.activeToolName != "" && m.activeToolName == ev.ToolName {
			prefix += " · " + fmtDuration(time.Since(m.activeToolStart))
		}
		prefix += ": "
		// Syntax-highlight fenced code in results (#5), else truncated plain text.
		// Skip the Markdown path for cat -n line-numbered output (Read previews):
		// glamour treats it as prose and reflows every line into one run-on block,
		// inlining the line numbers — show such results verbatim instead.
		if !ev.IsError && strings.Contains(s, "```") && len(s) <= 4000 && !looksLineNumbered(s) {
			m.appendLine(toolStyle.Render("  " + prefix))
			m.appendLine(m.markdown(s))
		} else {
			clipped, dropped := clipPreview(s, maxPreviewLines, maxPreviewRunes)
			if dropped > 0 {
				// Name the sequence number, so someone scrolling back through
				// the session can see exactly which one to ask for.
				clipped += fmt.Sprintf("\n…  %s", hintStyle.Render(
					fmt.Sprintf("(%d more lines · /last %d for full output)", dropped, seq)))
			}
			m.appendLine(style.Render("  " + prefix + strings.ReplaceAll(clipped, "\n", "\n  ")))
		}
		kind := navCommand
		if ev.IsError && !ev.HostBlocked {
			kind = navError
		}
		m.noteNav(kind, ev.ToolName+toolSummaryWith(ev.ToolName, ev.Input, m.displayPaths)+" → "+oneline(s, 60), "", seq)
		m.activeToolName = ""
		m.phase = "thinking"
		if m.loopRemaining > 0 && !m.loopPlan {
			m.refreshGoalList() // the model may just have ticked a box
		}
	case "compaction":
		m.flushAssistant()
		if ev.Content == "" {
			// Start of a slow (model-based) autocompact — show progress until
			// the matching done banner arrives. Microcompact never sends this.
			m.phase = "compacting"
		} else {
			// Completion banner. The compaction is already finished by the time
			// this arrives; what follows is the model turn, so leave the phase
			// as "thinking" rather than stranding it on "compacting" while we
			// wait on the (post-compaction, still large) request's first token.
			m.appendLine(bannerStyle.Render("· " + ev.Content))
			m.phase = "thinking"
		}
	case "notice":
		// A system notice the user must actually see — something the model was
		// told about but that leaves no visible trace of its own, like a web
		// search that was dropped from the conversation. Not faint: the point
		// of it is that the silence was the bug.
		m.flushAssistant()
		if ev.Content != "" {
			m.appendLine(noticeStyle.Render("! " + ev.Content))
			m.noteNav(navError, oneline(ev.Content, 60), "", 0)
		}
	case "plan":
		if ev.Plan != nil {
			m.plan = *ev.Plan
		}
	case "usage":
		// One inner LLM call's usage. Update both the session counters and the
		// per-turn tally so doneMsg's reconciliation knows what we already
		// counted. No scrollback line — the status bar at the bottom shows
		// the running totals, and we don't want a stream of "+1234 tokens"
		// noise during an iteration.
		m.statTurns += ev.TurnDelta
		m.statIn += ev.InputDelta
		m.statOut += ev.OutputDelta
		m.turnLiveTurns += ev.TurnDelta
		m.turnLiveIn += ev.InputDelta
		m.turnLiveOut += ev.OutputDelta
		// Demote streaming → thinking when a usage event arrives mid-turn —
		// it signals the model has finished one inner call. Don't clobber a
		// "running <Tool>" phase, since a usage event between tool_use and
		// tool_result is just the assistant's pre-tool turn-finalisation.
		if m.phase == "streaming" {
			m.phase = "thinking"
		}
	}
}

// Inline preview budget for a tool result. Counted in lines first and runes
// second: a flat byte budget produced an unreadable ribbon for multi-line
// output, and slicing bytes cut multi-byte characters in half.
const (
	maxPreviewLines = 8
	maxPreviewRunes = 480
)
