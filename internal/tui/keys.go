package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// interruptTurn cancels the in-flight turn. The cancelled context unblocks the
// agent goroutine (including any approval/question/plan prompt it is parked on,
// since those select on ctx.Done()), which then sends doneMsg. Shared by Esc and
// the first press of Ctrl+C so the two can never drift apart.
func (m *Model) interruptTurn() {
	m.turnCancel()
	m.turnCancel = nil
	m.loopRemaining, m.loopWrapUp, m.loopStubFixing, m.loopVerifying = 0, false, false, false // also halts a goal loop
	// Nothing is written to scrollback here. The partial reply is still in
	// the stream buffer, so a line now landed above the text it interrupted;
	// the live view says "cancelling… (Ctrl+C to force quit)" meanwhile, and
	// doneMsg writes "⊘ interrupted after …" below the flushed text.
	m.cancelling = true
}

// onCtrlC implements two-press Ctrl+C. A terminal user's reflex is that Ctrl+C
// stops the thing that is running, not that it destroys the session — so the
// first press always does the smallest useful thing and only an immediately
// repeated press quits. Resolution order:
//
//  1. armed by a previous press → quit;
//  2. a turn is in flight → interrupt it (and arm, so a wedged turn can still
//     be escaped — this is what the "Ctrl+C again to force quit" hint promises);
//  3. a local y/n or picker prompt is open → cancel it;
//  4. the draft line is non-empty → clear it (readline's Ctrl+C);
//  5. otherwise → arm, and let the status bar say so.
//
// Note that the permission/ask/plan prompts are NOT handled in step 3: those
// only exist while a turn is running, so step 2 catches them and cancelling the
// turn context is what releases the blocked agent goroutine.
func (m *Model) onCtrlC() (tea.Model, tea.Cmd) {
	if m.quitArmed {
		return m, m.quit()
	}
	if m.turnCancel != nil {
		m.interruptTurn()
		m.quitArmed = true
		return m, nil
	}
	switch m.state {
	case stateAwaitingConfirm:
		m.confirmAction = nil
		m.setState(stateIdle)
		m.appendLine(toolStyle.Render("  → cancelled"))
		return m, nil
	case stateAwaitingChoice:
		m.closeChoice()
		m.appendLine(toolStyle.Render("  → cancelled"))
		return m, nil
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		m.input.Reset()
		m.syncInputHeight()
		return m, nil
	}
	m.quitArmed = true
	return m, nil
}

// onPaste inserts a bracketed paste. Small, tab-free pastes go in verbatim so
// the common case is unchanged; anything larger or tab-bearing is parked in the
// paste store and represented by a chip, which promptValue expands at submit.
func (m *Model) onPaste(text string) (tea.Model, tea.Cmd) {
	// Only the states that show an editable input accept a paste. Elsewhere
	// (y/n prompts, numbered pickers) it would be interpreted as a keystroke.
	// editableInput is shared with inputHeight so the two cannot disagree about
	// which states have a box — they did, and a paste into the other-answer box
	// vanished without trace.
	if !m.editableInput() {
		return m, nil
	}
	text = normalizeNewlines(text)
	if text == "" {
		return m, nil
	}
	if chipWorthy(text) {
		m.input.InsertString(m.pastes.add(text))
	} else {
		// Safe to hand to the widget: no tabs to mangle, and newline
		// replacement is the identity now that CRLF is normalised.
		m.input.InsertString(text)
	}
	m.syncInputHeight()
	return m, nil
}

// onInputKey handles the keys that belong to the text box itself rather than to
// the state that happens to be showing it: Tab completion, and the @-menu's
// arrows and Esc. It reports whether the key was consumed.
//
// Keyed off editableInput and called above every per-state block, because the
// box is the same box in each state that has one. Gating these on stateIdle is
// what left @ completion dead while the model was working — you could type a
// queued follow-up but not complete a path in it — and that is the same drift
// editableInput was introduced to stop, after the paste gate and inputHeight
// disagreed about stateAnsweringOther.
func (m *Model) onInputKey(msg tea.KeyPressMsg) bool {
	if !m.editableInput() {
		return false
	}
	// Tab completes a slash command when the line is a bare "/token", otherwise
	// an @<path> reference.
	if key.Matches(msg, m.keymap().Complete) {
		v := m.input.Value()
		if m.slashCommandsLive() && strings.HasPrefix(v, "/") && !strings.ContainsAny(v, " \t") {
			m.completeSlash()
		} else {
			m.completeAtPath()
		}
		return true
	}
	// The menu owns Up/Down while it is open — it is on screen and it is what
	// the keys obviously refer to. Esc closes it. Both fall through to history
	// navigation, the queued-message edit and the usual Esc handling when it is
	// not open.
	if !m.menuActive() {
		return false
	}
	switch {
	case key.Matches(msg, m.keymap().Prev):
		return m.movePathMenu(-1)
	case key.Matches(msg, m.keymap().Next):
		return m.movePathMenu(1)
	case key.Matches(msg, m.keymap().Dismiss):
		return m.dismissPathMenu()
	case key.Matches(msg, m.keymap().Submit):
		// Enter on a highlighted file means "that one". It used to fall
		// through to submit, so the half-typed prompt was sent with the
		// partial @token still in it and the menu was left open over the
		// reply.
		return m.acceptPathMenu()
	}
	return false
}

// onHistoryKey gives ↑/↓ the same meaning in every state that has a box: step
// through earlier input. Only on a single line, so the arrows still move the
// cursor inside a draft you are editing.
//
// While the model works, a queued follow-up is recalled first — it is the most
// recent thing you typed and it is about to be sent. That is a property of the
// queue being non-empty, not of the state, so it is asked of the queue. Before
// this, ↑ with nothing queued simply did nothing while a turn ran, and the
// answer box had no history at all.
func (m *Model) onHistoryKey(msg tea.KeyPressMsg) bool {
	if !m.editableInput() || m.input.LineCount() > 1 {
		return false
	}
	prev := key.Matches(msg, m.keymap().Prev)
	if !prev && !key.Matches(msg, m.keymap().Next) {
		return false
	}
	if prev {
		if t := m.steer.takeBack(); t != "" {
			m.input.SetValue(t)
			m.input.CursorEnd()
			m.syncInputHeight()
			return true
		}
	}
	m.navigateHistory(prev)
	m.syncInputHeight()
	return true
}

func (m *Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Any key other than Ctrl+C disarms a pending "press again to quit".
	if msg.String() != "ctrl+c" {
		m.quitArmed = false
	}

	// Ctrl+T toggles the plan panel, but only while there is one. The text
	// box binds Ctrl+T to transpose-characters, and taking the key outright
	// (as Claude Code does) would remove that from everyone who never makes a
	// plan. Ahead of onInputKey, which would otherwise hand it to the box.
	//
	// The text box's own keys, before any state dispatch. Esc reaching the menu
	// ahead of the interrupt is deliberate: closing a dropdown that is on screen
	// must not also kill the turn behind it. A second Esc does that.
	if snap, _, _ := m.workList(); msg.String() == "ctrl+t" && planVisible(snap) {
		m.planCollapsed = !m.planCollapsed
		return m, nil
	}
	if m.onInputKey(msg) {
		return m, nil
	}
	if m.onHistoryKey(msg) {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		return m.onCtrlC()
	case "esc":
		// Leaving follow mode comes first: Esc while watching a log means "stop
		// watching", not "kill the turn that started an hour ago". The job keeps
		// running either way — following is a view, not a lifecycle.
		if m.stopFollow() {
			return m, nil
		}
		// Esc on an open picker dismisses the picker, whatever is running
		// behind it: its hint says "esc to cancel", and a picker opened
		// mid-turn (/mode while Klaudia works) is not a request to stop the
		// turn. (From PR #67.)
		if m.state == stateAwaitingChoice {
			return m, m.onChoiceKey(msg)
		}
		// Interrupt the in-flight turn (and any pending approval/question it is
		// blocked on). The cancelled context unblocks the agent goroutine, which
		// then sends doneMsg.
		if m.turnCancel != nil {
			m.interruptTurn()
			return m, nil
		}
	}

	// What Return means depends on config (see keyAction). Computed once, so
	// the idle prompt and the queue-while-running path cannot disagree about
	// which chord sends — they did, in the first cut of this change.
	action := m.keymap().action(msg)
	// A trailing backslash turns the send into a newline, as in Claude Code.
	// It is the one newline chord no terminal can swallow: Option+Return
	// arrives as plain Return in Apple Terminal unless Option is set to act as
	// Meta — which on a UK keyboard costs Option+3, the # key — and Windows
	// Terminal keeps Alt+Enter for fullscreen.
	// Only where Return sends: with input.enter = "newline", Return already
	// inserts one and the send chord should send.
	if action == actionSubmit && msg.String() == "enter" && m.backslashContinues() {
		action = actionNewline
	}

	// No scrollback keys are bound. Klaudia renders inline, so PgUp/PgDn, the
	// wheel, Home/End, tmux copy mode and the terminal's own search all operate
	// on real scrollback — intercepting any of them would only take away a
	// behaviour the terminal already implements better.

	// While the model works, the input stays editable. A slash command runs
	// immediately (display/config ones apply now; the few that mutate history or
	// start a turn refuse until you interrupt). Plain text is queued as a
	// follow-up: Enter queues it; Enter again (empty) interrupts and sends it; ↑
	// edits it.
	if m.state == stateRunning {
		switch {
		case action == actionNewline:
			m.input.InsertString("\n")
			m.syncInputHeight()
			return m, nil
		case action == actionSubmit:
			in := m.readInput()
			if isBang(in.Display) {
				// Refused rather than queued. A "!" line means "run this here",
				// and silently sending it to the model instead would have it
				// run something like it through Bash — a different command, in
				// the sandbox, under the permission gate. The text stays in the
				// box so Esc then Enter does what was asked.
				m.appendLine(hintStyle.Render("  ! can't run while Klaudia is working — esc to interrupt, then send it"))
				return m, nil
			}
			asPrompt, isPrompt := m.slashAsPrompt(in)
			if strings.HasPrefix(in.Display, "/") && !isPrompt {
				m.input.Reset()
				m.pushHistory(in.Display)
				m.appendLine(userStyle.Render("› ") + in.Display)
				m.syncInputHeight()
				return m.handleSlash(in.Prompt)
			}
			if !in.Empty() {
				m.steer.add(asPrompt.Display, asPrompt.Prompt)
				m.input.Reset()
				m.syncInputHeight()
				// No scrollback line here. The queued state is transient — it
				// ends the moment the message is sent — so it belongs only in
				// the live region (renderQueuedHint), which is recomputed each
				// frame and clears itself. Writing it to scrollback via
				// appendLine baked a permanent, duplicate hint into the
				// transcript every time a message was queued.
				return m, nil
			}
			if m.turnCancel != nil {
				// Take the queued message out of the box BEFORE cancelling. The
				// agent loop drains the same box at its post-tool-batch poll, and
				// cancelling an in-flight command completes that batch — so a
				// poll that fires after the cancel would otherwise drain the
				// message into the dying turn, leaving it in history unanswered
				// while the UI goes idle (the reported bug). Draining here makes
				// that poll find nothing.
				if resend := m.steer.drain(); !resend.Empty() {
					m.interruptResend = resend
					m.turnCancel()
					m.turnCancel = nil
					m.cancelling = true // bottom view swaps to "cancelling…" so user sees the cancel registered
					m.flushAssistant()  // the notice belongs below the text it interrupts
					m.appendLine(toolStyle.Render("  ⊘ interrupting to send your queued message…"))
				}
			}
			return m, nil
		}
		return m, m.updateInput(msg)
	}

	if m.state == stateAwaitingPlan {
		// y and n keep their old meanings: approve and run, keep planning.
		choice := -1
		switch strings.ToLower(msg.String()) {
		case "y":
			choice = 0
		case "n":
			choice = 2
		default:
			choice = pickerKey(msg, len(planChoices), &m.planIdx, &m.planStray)
		}
		switch choice {
		case 0:
			m.approvePlan(false)
		case 1:
			m.approvePlan(true)
		case 2:
			return m, m.beginPlanFeedback()
		}
		return m, nil
	}

	if m.state == stateAwaitingConfirm {
		switch strings.ToLower(msg.String()) {
		case "y":
			action := m.confirmAction
			m.confirmAction = nil
			m.setState(stateIdle)
			if action != nil {
				m.appendLine(bannerStyle.Render(action()))
			}
		case "n":
			m.confirmAction = nil
			m.setState(stateIdle)
			m.appendLine(toolStyle.Render("  → cancelled"))
		}
		return m, nil
	}

	if m.state == stateAwaitingChoice {
		return m, m.onChoiceKey(msg)
	}

	if m.state == stateAwaitingAnswer {
		// 1..N are the model's options; N+1 is the "something else" escape
		// hatch, which opens free text rather than answering. As in Claude
		// Code, free text is one deliberate step away (see pickerKey).
		n := len(m.askOptions) + 1
		switch i := pickerKey(msg, n, &m.askIdx, &m.askStray); {
		case i < 0:
		case i == n-1:
			return m, m.beginOtherAnswer("")
		default:
			m.answerAsk(m.askOptions[i].Label)
		}
		return m, nil
	}

	if m.state == stateAnsweringOther {
		if msg.String() == "esc" { // back to the list, draft discarded
			m.input.Reset()
			m.syncInputHeight()
			if m.otherFor == otherPlan {
				m.setState(stateAwaitingPlan)
			} else {
				m.setState(stateAwaitingAnswer)
			}
			return m, nil
		}
		switch action {
		case actionNewline:
			m.input.InsertString("\n")
			m.syncInputHeight()
			return m, nil
		case actionSubmit:
			in := m.readInput()
			if in.Empty() {
				return m, nil // nothing to send yet
			}
			answer := in.Prompt
			m.input.Reset()
			m.syncInputHeight()
			m.pushHistory(answer)
			if m.otherFor == otherPlan {
				m.rejectPlan(answer)
			} else {
				m.answerAsk(answer)
			}
			return m, nil
		}
		return m, m.updateInput(msg)
	}

	if m.state == stateAwaitingPermission {
		host := hostgate.CardOf(m.pendingReq) != nil
		switch strings.ToLower(msg.String()) {
		case "y":
			m.answer(permission.Decision{Behavior: permission.Allow})
		case "n":
			msg := "denied by user"
			if host {
				msg = "the user declined this change to their machine"
			}
			m.answer(permission.Decision{Behavior: permission.Deny, Message: msg})
		case "s":
			// "Something else" is not "no". Someone declining a host change
			// usually wants the task done differently, not abandoned — and the
			// plain refusal tells the model to stop looking, which is right for
			// "no" and wrong here. The turn stays alive and whatever they type
			// next lands before Klaudia's next action, through the same steering
			// path a mid-turn correction uses.
			if !host {
				return m, nil
			}
			m.hostRedirect = true
			m.answer(permission.Decision{
				Behavior: permission.Deny,
				Message: "The user declined this change to their machine and is redirecting. " +
					"Do not look for another way to make it. They are about to say what they want " +
					"instead — wait for that instruction and follow it.",
			})
		}
		return m, nil
	}

	if action == actionNewline && m.state == stateIdle {
		m.input.InsertString("\n")
		m.syncInputHeight()
		return m, nil
	}

	if action == actionSubmit && m.state == stateIdle && isBang(m.input.Value()) {
		in := m.readInput()
		m.input.Reset()
		m.pushHistory(in.Display)
		m.syncInputHeight()
		m.setState(stateRunning)
		return m.runBang(in.Prompt)
	}

	if action == actionSubmit && m.state == stateIdle {
		in := m.readInput()
		if in.Empty() {
			return m, nil
		}
		m.input.Reset()
		m.pushHistory(in.Display)
		m.appendLine(userStyle.Render("› ") + in.Display)
		m.noteNav(navUser, in.Display, in.Prompt, 0)

		// Slash commands are handled locally, not sent to the model; a line
		// that only starts with a slash (a path) is a prompt (slashAsPrompt).
		// History keeps it as typed, so recalling it routes the same way.
		asPrompt, isPrompt := m.slashAsPrompt(in)
		if strings.HasPrefix(in.Display, "/") && !isPrompt {
			return m.handleSlash(in.Prompt)
		}
		in = asPrompt

		m.setState(stateRunning)
		// Do NOT arm another waitForEvent here: exactly one channel reader must
		// be outstanding (Init armed it; each channel-event handler re-arms it).
		// A second reader would race and deliver streamed deltas out of order.
		// startTurn returns only spinner/stopwatch ticks (separate cmd loops).
		return m, m.startTurn(in.Prompt)
	}

	return m, m.updateInput(msg)
}

// backslashContinues reports whether the input ends in a backslash, and if
// so removes it, so the Return that follows inserts a newline instead of
// sending. Only at the end of the input: a backslash typed mid-line is part of
// what the user is writing (a path, a regex), not a request for a new line.
func (m *Model) backslashContinues() bool {
	v := m.input.Value()
	if !strings.HasSuffix(v, "\\") || !m.cursorAtEnd() {
		return false
	}
	m.input.SetValue(strings.TrimSuffix(v, "\\"))
	return true
}

// cursorAtEnd reports whether the cursor is after the last character.
func (m *Model) cursorAtEnd() bool {
	lines := strings.Split(m.input.Value(), "\n")
	last := lines[len(lines)-1]
	return m.input.Line() == len(lines)-1 && m.input.LineInfo().StartColumn+m.input.LineInfo().CharOffset == len([]rune(last))
}

// quit leaves Klaudia: one line saying how to come back to this session, then
// an empty frame and exit. Sequenced, not batched — a batched Println could
// lose the race with Quit and never be printed.
func (m *Model) quit() tea.Cmd {
	m.quitting = true
	if len(m.history) > 0 && m.sess != nil && m.sess.SessionID != "" {
		m.appendLine(hintStyle.Render("Session saved — run klaudia here to pick it up again, or klaudia -r " +
			m.sess.SessionID + " for this one."))
	}
	return tea.Sequence(m.out.drainCmd(), tea.Quit)
}
