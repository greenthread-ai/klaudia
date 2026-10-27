package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// intro is the welcoming banner shown at startup. The model name/branch/session
// id are filled in by the caller.
// intro renders the startup banner. The session id is intentionally absent:
// interactive runs auto-resume the most recent project session, so reciting it
// (and a manual `--resume` command) on every launch is noise; /status surfaces
// it on demand.
// skillNames lists the loaded skills for the banner. Sourced from the same
// slash-command list the completer uses, so the banner cannot disagree with
// what /<name> will actually dispatch.
func skillNames(sess *Session) []string {
	if sess == nil {
		return nil
	}
	names := make([]string, 0, len(sess.Skills))
	for _, sk := range sess.Skills {
		names = append(names, sk.Name)
	}
	sort.Strings(names)
	return names
}

// introSkillsShown caps the names listed before the line becomes noise.
const introSkillsShown = 4

func intro(model, branch, tagline string, skills []string, submit key.Binding) string {
	logo := logoStyle.Render("✦ Klaudia")
	tag := bannerStyle.Render(" " + tagline)
	var meta string
	if model != "" {
		meta = "\n" + bannerStyle.Render("  model: "+model)
	}
	if branch != "" {
		meta += bannerStyle.Render("   ⎇ " + branch)
	}
	// Skills were the thing nobody could tell was working: with none loaded
	// there is no Skill tool to ask about, and with some loaded the only
	// evidence was asking the model. One line at startup settles it.
	if len(skills) > 0 {
		shown := skills
		suffix := ""
		if len(shown) > introSkillsShown {
			suffix = fmt.Sprintf(" +%d more", len(shown)-introSkillsShown)
			shown = shown[:introSkillsShown]
		}
		meta += "\n" + bannerStyle.Render("  skills: "+strings.Join(shown, ", ")+suffix)
	}
	// The send chord comes from the binding, not from the word "Enter": with
	// input.enter = "newline" Return inserts a newline and this line was telling
	// every one of those users to press the key that does not send.
	//
	// Kept under 80 columns: at 103 it wrapped on every terminal of ordinary
	// width, and the whole of /help is one keystroke away.
	tip := hintStyle.Render("\n  " + chordCase(submit.Help().Key) +
		" sends · / commands · @ files · Esc stops · Ctrl+C twice quits")
	return logo + tag + meta + tip + "\n"
}

// fmtDuration renders a turn duration compactly: "850ms", "12.3s", "2m05s".
func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
}

// throughput renders the token count and tokens/sec for a completed turn, or ""
// when no output tokens were reported (e.g. some OpenAI-compatible endpoints).
func throughput(outTokens int64, d time.Duration) string {
	if outTokens <= 0 {
		return ""
	}
	s := " · " + humanTokens(outTokens) + " tokens"
	if d > 0 {
		s += fmt.Sprintf(" · %.0f tok/s", float64(outTokens)/d.Seconds())
	}
	return s
}

// humanTokens renders a token count compactly: 980 → "980", 1240 → "1.2k".
func humanTokens(n int64) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		// Million-token context windows are ordinary now; without this tier a
		// 1M window renders as the unreadable "1000.0k".
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
}

// capitalize upper-cases the first rune (role headers in the export).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// renderQueuedHint composes the "queued: <message>" line shown under the
// input while the model is working. The message snippet is rendered in the
// user-input style (Bold/accent2) so it's scannable at a glance — the
// previous all-dim version was easy to miss during long turns, and the
// session that lost a "ive setup caddy…" message to a stuck Bash showed
// the cost. Wrapper text (label, key hints, line count) stays in hint
// style so the visual weight goes to the queued content itself.
func (m *Model) renderQueuedHint() string {
	text, halt := m.steer.peek()
	if text == "" && halt {
		return hintStyle.Render("⏎ stopping after the current step…")
	}
	snippet := oneline(text, 60)
	// The label carries the reassurance that used to be a separate scrollback
	// line: the message is not lost while Klaudia works — it is read at the next
	// step without interrupting. Enter escalates to interrupting now.
	label := hintStyle.Render("⏎ queued (read at next step): ")
	body := userStyle.Render(snippet)
	tail := "  " + hintStyle.Render("(Enter interrupts now · ↑ edits)")
	if lines := strings.Count(text, "\n") + 1; lines > 1 {
		tail = "  " + hintStyle.Render(fmt.Sprintf("(%d lines · Enter interrupts now · ↑ edits)", lines))
	}
	if halt {
		tail += "  " + hintStyle.Render("· stopping after this step")
	}
	return label + body + tail
}

// phaseLabel returns the verb shown in the spinner row. Cancellation
// outranks everything (so Esc/queued-Enter gets the dominant feedback);
// otherwise we surface whatever phase renderEvent last set, defaulting to
// "working" before the first event has landed in a turn. When in an active
// tool, the elapsed wall-clock of that specific call is appended so a
// long-running tool is visible separately from the turn timer — for the
// "ran two quick tools, then this one stalled" case the spinner reads
// "running Bash 42s… 1m12s" and the user can tell the LATEST call is the
// stalled one rather than the whole turn.
func (m *Model) phaseLabel() string {
	switch {
	case m.cancelling:
		return "cancelling"
	case m.phase == "":
		return "working"
	case m.activeToolName != "":
		return "running " + m.activeToolName + " " + fmtDuration(time.Since(m.activeToolStart))
	default:
		return m.phase
	}
}

// resize re-measures the live region. It deliberately does not reflow anything
// already printed: that text belongs to the terminal now, and whether it rewraps
// on resize is the terminal's business (iTerm2 does, tmux does not — both are
// correct). Only subsequent output picks up the new width.
func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.ready = true
	for _, b := range m.unsized { // committed before the size was known (see commit)
		m.out.push(m.linkify(m.fitScrollback(b.rendered), b.text))
	}
	m.unsized = nil
	// Children have no terminal of their own, so COLUMNS/LINES is the only way
	// a command's output wraps to the width the user is actually looking at
	// rather than the 80 columns a pipe implies.
	sandbox.SetTerminalSize(w, h)
	m.input.SetWidth(m.inputWidth())
	if m.glam == nil || m.glamWidth != w {
		m.buildGlamour(w)
	}
	m.syncInputHeight()
}

func (m *Model) introText() string {
	return intro(m.introModel, m.introBranch, m.introTagline, skillNames(m.sess), m.keymap().Submit)
}

// View draws only the live region. Everything finished has already been printed
// into the terminal's scrollback by the print queue.
func (m *Model) View() string {
	if !m.ready || m.quitting {
		// Quitting draws nothing, so the final frame erases the live region:
		// the input box and status line used to stay behind in scrollback.
		return ""
	}

	out := m.clampBottom(m.fitLiveRegion(m.bottomView()))
	// Record what this frame measures, so the next resize can work out how far
	// the terminal will have reflowed it.
	m.noteFrame(out)
	if m.pendingReflow != "" {
		out, m.pendingReflow = m.pendingReflow+out, ""
	}
	if m.pendingOSC != "" {
		out, m.pendingOSC = m.pendingOSC+out, ""
	}
	return out
}

// noteFrame records each line's visual width and the width it was drawn at.
func (m *Model) noteFrame(frame string) {
	lines := strings.Split(frame, "\n")
	widths := make([]int, len(lines))
	for i, ln := range lines {
		widths[i] = ansi.StringWidth(ln)
	}
	m.lastFrame, m.lastFrameWidth = widths, m.width
}

// fitLiveRegion wraps the live region so no line reaches the terminal's last
// column.
//
// This is the invariant the whole inline renderer rests on, and it has now been
// broken twice by different components, so it is enforced in one place rather
// than by each of them remembering.
//
// Why it matters: Bubble Tea returns to the top of the live region with
// CursorUp(linesRendered-1), counting *logical* lines. A line that fills the
// width is two physical rows, so the cursor lands inside the region; the next
// tea.Println then writes over the middle of it and strands the rows above in
// scrollback for good. That is the stray "› Ask Klaudia…" and status line seen
// in a real session. The renderer also only appends EraseLineRight to lines
// narrower than the terminal, so a full-width line never cleans up after
// itself.
//
// Wrapping rather than truncating: a long path in an approval prompt is worth
// reading, and the extra rows are handled by clampBottom, which runs after.
func (m *Model) fitLiveRegion(s string) string {
	limit := m.width - 1
	if limit < 20 || s == "" {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if ansi.StringWidth(line) < limit {
			out = append(out, line)
			continue
		}
		out = append(out, strings.Split(ansi.Hardwrap(line, limit, true), "\n")...)
	}
	return strings.Join(out, "\n")
}

// clampBottom keeps the live region strictly shorter than the terminal. Bubble
// Tea's inline renderer drops lines off the TOP of an over-tall frame
// (standard_renderer.go), which would silently eat the status bar and input, so
// we trim from the top ourselves — sacrificing the streaming preview first and
// always keeping the input and status bar visible.
func (m *Model) clampBottom(s string) string {
	budget := m.height - 1
	if budget < 1 {
		budget = 1
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= budget {
		return s
	}
	return strings.Join(lines[len(lines)-budget:], "\n")
}

// bottomView renders everything below the scrollback: the state-specific prompt
// or input area, then the persistent status bar. Its measured height is what
// resize reserves, so the two can never drift.
func (m *Model) bottomView() string {
	var bottom string
	// The plan panel sits directly above the input, while idle and while
	// running — nowhere else. Every other state is a question whose options
	// were just printed in scrollback directly above its prompt, and a
	// checklist wedged between the two reads as part of the question.
	snap, title, suffix := m.workList()
	panel := renderWorkList(snap, title, suffix, m.planCollapsed, m.width)
	withPanel := func(s string) string {
		if panel == "" {
			return s
		}
		return panel + "\n" + s
	}
	switch m.state {
	case stateRunning:
		label := " " + m.phaseLabel() + "… "
		hint := "  (esc to interrupt)"
		if m.cancelling {
			hint = "  (Ctrl+C to force quit)"
		}
		work := m.spin.View() + label + bannerStyle.Render(m.sw.View()) + hintStyle.Render(hint)
		// Surface "stuck" by tracking time since the last event the renderer
		// saw. Suppressed during cancellation — "cancelling… quiet for 12s"
		// is a worse signal than "cancelling…". 30s threshold picked so a
		// fast turn never sees the suffix; a long API call or tool-bound
		// stall does. Re-evaluated every spinner tick (spinner.Dot, 100ms).
		if !m.cancelling && !m.lastEventAt.IsZero() {
			if q := time.Since(m.lastEventAt); q > 30*time.Second {
				work += hintStyle.Render(fmt.Sprintf("  · quiet for %s", fmtDuration(q)))
			}
		}
		m.input.SetHeight(m.inputHeight())
		bottom = withPanel(caption(work) + "\n" + m.promptBox() + m.inputAffordanceView())
		// Above the panel, so clampBottom — which trims from the top — gives
		// up the streaming preview before any of the plan.
		//
		// Show the not-yet-committed tail of the streaming message above the
		// working line, so the user sees text arriving even though the finished
		// part has already gone to scrollback.
		if tail := m.streamTail(); tail != "" {
			bottom = tail + "\n" + bottom
		}
		if m.steer.pending() {
			bottom += "\n" + caption(m.renderQueuedHint())
		}
	case stateAwaitingPermission:
		bottom = caption(askStyle.Render(m.permissionPrompt()))
	case stateAwaitingAnswer:
		bottom = m.askView()
	case stateAnsweringOther:
		m.input.SetHeight(m.inputHeight())
		title := "Your answer"
		if m.otherFor == otherPlan {
			title = "What should change?"
		}
		bottom = m.promptBox() + m.inputAffordanceView() + "\n" + caption(askStyle.Render(title)+
			hintStyle.Render("  (enter sends · esc goes back to the options)"))
	case stateAwaitingPlan:
		bottom = caption(askStyle.Render("Approve this plan?")) + "\n" + m.pickerView(planChoices, m.planIdx, m.planStray, 2)
	case stateAwaitingConfirm:
		bottom = caption(askStyle.Render("Confirm? (y)es / (n)o"))
	case stateAwaitingChoice:
		bottom = m.choiceView()
	default:
		m.input.SetHeight(m.inputHeight())
		bottom = withPanel(m.promptBox() + m.inputAffordanceView())
	}
	// Running work sits under the prompt, while idle and while running: in a
	// question's states it would read as one of the answers.
	if m.state == stateIdle || m.state == stateRunning {
		if jobs := m.jobsPanel(); jobs != "" {
			bottom += "\n" + jobs
		}
	}
	// Persistent status bar at the very bottom, in every state.
	return bottom + "\n" + caption(m.statusLine())
}

// inputAffordanceView renders what hangs below the text box: the slash
// suggestion line and the @-completion menu. Every state that shows the box
// calls this one function, so the dropdown cannot be visible in one state and
// invisible in another — which is exactly how it shipped, live on the idle line
// and absent while the model worked.
func (m *Model) inputAffordanceView() string {
	var out string
	if m.slashCommandsLive() {
		if sug := m.slashSuggestionLine(); sug != "" {
			out += "\n" + caption(sug)
		}
	}
	if menu := m.pathMenuView(); menu != "" {
		out += "\n" + menu
	}
	return out
}

// countOf writes n with word in the right number: "1 file", "2 files". It
// replaces "%d file(s)", which reads as a form being filled in.
func countOf(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
