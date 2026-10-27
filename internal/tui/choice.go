package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
func (m *Model) onChoiceKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.closeChoice()
		m.appendLine(toolStyle.Render("  → cancelled"))
		return nil
	case tea.KeyEnter:
		m.applyChoice(m.choiceIdx)
		return nil
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveChoice(-1)
		return nil
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveChoice(1)
		return nil
	case tea.KeyBackspace:
		if m.choiceFilter != "" {
			m.choiceFilter = trimLastRune(m.choiceFilter)
			m.refilterChoices()
		}
		return nil
	case tea.KeySpace:
		m.choiceFilter += " "
		m.refilterChoices()
		return nil
	case tea.KeyRunes:
		if s := string(msg.Runes); len(s) == 1 && s[0] >= '1' && s[0] <= '9' && m.digitsPick() {
			m.applyChoice(int(s[0] - '1'))
			return nil
		}
		m.choiceFilter += string(msg.Runes)
		m.refilterChoices()
		return nil
	}
	return nil
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
	m.appendLine(bannerStyle.Render("  → " + item.apply()))
}

// closeChoice clears the picker and returns the keyboard to the prompt.
func (m *Model) closeChoice() {
	m.choiceItems, m.choiceHits, m.choicePrompt, m.choiceFilter, m.choiceIdx = nil, nil, "", "", 0
	m.setState(stateIdle)
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

// askView renders a pending AskUserQuestion's options as a picker: the
// model's options, then the escape hatch (see otherAnswerLabel), the
// highlighted one marked, and a hint that names every key that works.
func (m *Model) askView() string {
	var b strings.Builder
	width := max(m.width-8, 20)
	n := len(m.askOptions) + 1
	for i := 0; i < n; i++ {
		label := otherAnswerLabel // always last, always present
		if i < len(m.askOptions) {
			label = m.askOptions[i].Label
			if d := m.askOptions[i].Description; d != "" {
				label += " — " + d
			}
		}
		num := ""
		if i < 9 {
			num = strconv.Itoa(i+1) + ") "
		}
		label = ansi.Truncate(label, width, "…")
		if i == m.askIdx {
			b.WriteString(caption(suggestStyle.Render("❯ " + num + label)))
		} else {
			b.WriteString(caption(toolStyle.Render("  " + num + label)))
		}
		b.WriteString("\n")
	}
	hint := "↑↓ or 1-" + strconv.Itoa(min(n, 9)) + " to pick · enter to confirm · esc to interrupt"
	if m.askStray {
		hint = "typing doesn't answer — pick " + strconv.Itoa(n) + " to write your own · ↑↓ or 1-" + strconv.Itoa(min(n, 9)) + " to pick"
	}
	b.WriteString(caption(hintStyle.Render("  " + hint)))
	return b.String()
}
