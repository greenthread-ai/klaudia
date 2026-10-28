package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/greenthread-ai/klaudia/internal/fuzzy"
)

// The settings picker (/model, /theme, /mode, /mcp).
//
// It used to print a numbered list into scrollback and wait for a digit, which
// meant it could only ever offer nine things. /model is the one that outgrew
// that: an OpenAI-compatible endpoint serves dozens of models, so the list was
// truncated to nine and the rest were reachable only by typing an id — the one
// thing the picker exists to spare you (model_picker.go).
//
// So it is now the same live dropdown the @ menu uses: arrow-selectable, typing
// filters, windowed to a few rows with an N/M counter. Digits still pick the
// first nine, which is what every short list (themes, modes) still is.
//
// Like the queued-message hint, and unlike the old version, it renders only in
// the live region. A filterable list printed into scrollback would be frozen at
// whatever it said when it was drawn, and scrollback cannot be revised.

const choiceRows = 8

// digitsPick reports whether 1-9 select a row rather than extend the filter.
// They cannot do both: model ids are full of digits ("claude-opus-5"), so once
// the user is filtering, a digit is a character they typed and nothing else.
// With an empty filter — every short list, and the first keystroke on a long
// one — the old numbered picker's muscle memory still works, and choiceView
// shows the numbers only while that is true.
func (m *Model) digitsPick() bool { return m.choiceFilter == "" }

// onChoiceKey is the picker's whole keyboard. It returns a command rather than
// a bool because, unlike the @ menu, the picker owns every key while it is
// open: there is no text box underneath for an unhandled key to fall through
// to, and a stray character must filter rather than vanish.
func (m *Model) onChoiceKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.closeChoice()
		m.appendLine(toolStyle.Render("  → cancelled"))
		return nil
	case "enter":
		m.applyChoice(m.choiceIdx)
		return nil
	case "up", "ctrl+p":
		m.moveChoice(-1)
		return nil
	case "down", "ctrl+n":
		m.moveChoice(1)
		return nil
	case "backspace":
		if m.choiceFilter != "" {
			m.choiceFilter = trimLastRune(m.choiceFilter)
			m.refilterChoices()
		}
		return nil
	}
	if s := typedText(msg); s != "" {
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' && m.digitsPick() {
			m.applyChoice(int(s[0] - '1'))
			return nil
		}
		m.choiceFilter += s
		m.refilterChoices()
	}
	return nil
}

// typedText is the text a keypress types, or "" for a key that types none
// (an arrow, a ctrl chord). Shift only changes the text, so it still counts.
func typedText(msg tea.KeyPressMsg) string {
	if msg.Mod&^tea.ModShift != 0 {
		return ""
	}
	return msg.Text
}

// trimLastRune drops the final rune, so backspacing a filter never splits a
// multi-byte character into invalid UTF-8.
func trimLastRune(s string) string {
	rs := []rune(s)
	if len(rs) == 0 {
		return s
	}
	return string(rs[:len(rs)-1])
}

// refilterChoices recomputes which items match the typed filter, newest typing
// first. An empty filter matches everything, in the order the caller supplied —
// that order is meaningful (models arrive newest-first) and must survive.
func (m *Model) refilterChoices() {
	m.choiceHits = m.choiceHits[:0]
	if m.choiceFilter == "" {
		for i := range m.choiceItems {
			m.choiceHits = append(m.choiceHits, i)
		}
		m.choiceIdx = 0
		return
	}
	pat := strings.ToLower(m.choiceFilter)
	type scored struct{ idx, score int }
	var hits []scored
	for i, it := range m.choiceItems {
		if score, ok := fuzzy.Subsequence(pat, strings.ToLower(it.label)); ok {
			hits = append(hits, scored{i, score})
		}
	}
	// Stable by score, then by the caller's order, so a filter never shuffles
	// equally-good matches around as you type.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].score > hits[j-1].score; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	for _, h := range hits {
		m.choiceHits = append(m.choiceHits, h.idx)
	}
	m.choiceIdx = 0
}

// moveChoice steps the selection, wrapping.
func (m *Model) moveChoice(delta int) {
	if n := len(m.choiceHits); n > 0 {
		m.choiceIdx = ((m.choiceIdx+delta)%n + n) % n
	}
}

// applyChoice runs the nth filtered item and closes the picker. The result line
// is the only part that reaches scrollback, because it is the only part that is
// still true afterwards.
func (m *Model) applyChoice(n int) {
	if n < 0 || n >= len(m.choiceHits) {
		return
	}
	item := m.choiceItems[m.choiceHits[n]]
	m.closeChoice()
	// An item that goes on to ask something (a rewind's confirmation) prints
	// its own text and returns nothing.
	if s := item.apply(); s != "" {
		m.appendLine(bannerStyle.Render("  → " + s))
	}
}

// closeChoice clears the picker and returns to the state it opened over:
// the prompt when idle, the running turn when it opened mid-turn.
func (m *Model) closeChoice() {
	m.choiceItems, m.choiceHits, m.choicePrompt, m.choiceFilter, m.choiceIdx = nil, nil, "", "", 0
	m.setState(m.choiceReturn)
	m.choiceReturn = stateIdle
}

// closeChoiceForPrompt dismisses an open picker because the running turn now
// needs an answer (approval, question, plan). That prompt takes the bottom
// view; the picker's items are dropped rather than left stale, and the user is
// told it closed so the missing picker is not a mystery.
func (m *Model) closeChoiceForPrompt() {
	if m.state != stateAwaitingChoice {
		return
	}
	m.closeChoice()
	m.appendLine(toolStyle.Render("  → picker closed: Klaudia needs an answer first"))
}

// settleState moves to s when background work (a turn, a compaction, a !
// command) finishes or a follow-up turn starts. If a picker is open over that
// work it stays open, and s becomes the state it returns to; otherwise the
// picker would vanish under the user's fingers and a later digit would land in
// the input box.
func (m *Model) settleState(s uiState) {
	if m.state == stateAwaitingChoice {
		m.choiceReturn = s
		return
	}
	m.setState(s)
}

// choiceView renders the picker: title, a window of rows around the selection,
// and a hint line carrying the filter and the count.
func (m *Model) choiceView() string {
	var b strings.Builder
	title := m.choicePrompt
	if m.choiceFilter != "" {
		title += "  " + suggestStyle.Render(m.choiceFilter)
	}
	b.WriteString(caption(askStyle.Render(title)) + "\n")

	if len(m.choiceHits) == 0 {
		b.WriteString(caption(hintStyle.Render("  no match for " + strconv.Quote(m.choiceFilter) + " · backspace to widen · esc to cancel")))
		return b.String()
	}

	// Leave room for the title, the hint, the status bar and a little
	// scrollback, so a short terminal shows a shorter list rather than having
	// the top of it clipped away by clampBottom.
	rows := min(choiceRows, len(m.choiceHits))
	if room := m.height - 5; room > 0 && rows > room {
		rows = room
	}
	start := 0
	if m.choiceIdx >= rows {
		start = m.choiceIdx - rows + 1
	}
	width := m.width - 8
	if width < 20 {
		width = 20
	}
	for i := start; i < start+rows && i < len(m.choiceHits); i++ {
		// The digit is shown because the digit still works — the mapping has to
		// be visible for that to be true, and invisible the moment a filter
		// takes the digits back (digitsPick).
		num := ""
		if m.digitsPick() && i < 9 {
			num = strconv.Itoa(i+1) + ") "
		}
		label := ansi.Truncate(m.choiceItems[m.choiceHits[i]].label, width, "…")
		if i == m.choiceIdx {
			b.WriteString(caption(suggestStyle.Render("❯ " + num + label)))
		} else {
			b.WriteString(caption(toolStyle.Render("  " + num + label)))
		}
		b.WriteString("\n")
	}

	hint := "↑↓ to pick · enter to confirm · type to filter · esc to cancel"
	if m.digitsPick() {
		hint = "↑↓ or 1-9 to pick · enter to confirm · type to filter · esc to cancel"
	}
	if len(m.choiceHits) > rows {
		hint = strconv.Itoa(m.choiceIdx+1) + "/" + strconv.Itoa(len(m.choiceHits)) + "  " + hint
	}
	b.WriteString(caption(hintStyle.Render("  " + hint)))
	return b.String()
}

// The answer pickers — AskUserQuestion's options and plan approval — share one
// keyboard and one look with each other and with the settings picker above:
// ↑↓ (or Ctrl+P/N) move, Enter takes the highlighted row, digits pick
// directly. Unlike the settings picker they do not filter: their rows are
// sentences, few, and typed text is ignored and explained instead (see
// pickerKey on why that matters).

// pickerKey applies a picker key to idx/stray and returns the chosen row, or
// -1 when the key chose nothing.
//
// Typing is ignored. It used to start a free-text answer, so a message typed
// ahead while the model worked became the answer the moment a question
// appeared — the user answered a question they had not read. And the Enter
// that follows stray text is swallowed: text then Enter is someone finishing
// a message, not confirming row one.
func pickerKey(msg tea.KeyPressMsg, n int, idx *int, stray *bool) int {
	switch msg.String() {
	case "up", "ctrl+p":
		*idx, *stray = (*idx-1+n)%n, false
		return -1
	case "down", "ctrl+n":
		*idx, *stray = (*idx+1)%n, false
		return -1
	case "enter":
		if *stray {
			*stray = false
			return -1
		}
		return *idx
	}
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		if i := int(s[0] - '1'); i < n {
			return i
		}
		return -1 // a digit past the list: ignore rather than guess
	}
	if typedText(msg) != "" {
		*stray = true
	}
	return -1
}

// pickerView renders rows with the highlighted one marked, then a hint naming
// every key that works. writeRow names the row that opens free text, so the
// "typing doesn't answer" hint can point at it.
func (m *Model) pickerView(rows []string, idx int, stray bool, writeRow int) string {
	var b strings.Builder
	width := max(m.width-8, 20)
	for i, label := range rows {
		num := ""
		if i < 9 {
			num = strconv.Itoa(i+1) + ") "
		}
		label = ansi.Truncate(label, width, "…")
		if i == idx {
			b.WriteString(caption(suggestStyle.Render("❯ " + num + label)))
		} else {
			b.WriteString(caption(toolStyle.Render("  " + num + label)))
		}
		b.WriteString("\n")
	}
	keys := "↑↓ or 1-" + strconv.Itoa(min(len(rows), 9)) + " to pick"
	hint := keys + " · enter to confirm · esc to interrupt"
	if stray {
		hint = "typing doesn't answer — pick " + strconv.Itoa(writeRow+1) + " to write your own · " + keys
	}
	b.WriteString(caption(hintStyle.Render("  " + hint)))
	return b.String()
}

// askView renders a pending AskUserQuestion: the model's options, then the
// escape hatch (see otherAnswerLabel), always last and always present.
func (m *Model) askView() string {
	rows := make([]string, 0, len(m.askOptions)+1)
	for _, o := range m.askOptions {
		label := o.Label
		if o.Description != "" {
			label += " — " + o.Description
		}
		rows = append(rows, label)
	}
	rows = append(rows, otherAnswerLabel)
	return m.pickerView(rows, m.askIdx, m.askStray, len(rows)-1)
}
