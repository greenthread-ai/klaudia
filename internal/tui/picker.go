package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A settings picker (/mode, /theme, /mcp, /model) used to print its items into
// scrollback, numbered, and read a single digit. That capped every picker at
// nine items — /model truncated the provider's list to fit — and an item's
// number was the only way to reach it. The list now lives in the live region
// instead, where it can be redrawn: a highlighted row moved with ↑/↓, a filter
// narrowed as you type, and a window of rows that scrolls when the list is
// taller than the terminal. The digits still pick directly while no filter is
// typed, so the muscle memory for "/mode, 2" keeps working.

// choiceNav is the keyboard state of an open picker. cursor indexes the
// filtered list (choiceMatches), not choiceItems; offset is the first filtered
// row in the visible window.
type choiceNav struct {
	filter string
	cursor int
	offset int
}

// maxPickerRows caps the window on a tall terminal. A picker is a glance, not
// a page: past this the region mostly repaints rows nobody reads, and the
// filter is the faster way down a long list anyway.
const maxPickerRows = 15

// choiceMatches returns the indices of the items the filter keeps. Every
// space-separated word of the filter must appear in the label, ignoring case,
// so "opus 4" finds "Claude Opus 4.5  claude-opus-4-5".
func (m *Model) choiceMatches() []int {
	words := strings.Fields(strings.ToLower(m.choiceNav.filter))
	out := make([]int, 0, len(m.choiceItems))
	for i, it := range m.choiceItems {
		label := strings.ToLower(it.label)
		keep := true
		for _, w := range words {
			if !strings.Contains(label, w) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, i)
		}
	}
	return out
}

// onChoiceKey handles a key while a picker is open.
func (m *Model) onChoiceKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	nav := &m.choiceNav
	matches := m.choiceMatches()
	switch msg.Type {
	case tea.KeyEsc:
		m.closeChoice()
		m.appendLine(toolStyle.Render("  → cancelled"))
		return m, nil
	case tea.KeyEnter:
		if nav.cursor < len(matches) {
			m.pickChoice(matches[nav.cursor])
		}
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveChoice(-1, len(matches))
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveChoice(1, len(matches))
		return m, nil
	case tea.KeyPgUp:
		m.moveChoice(-m.choiceRows(len(matches)), len(matches))
		return m, nil
	case tea.KeyPgDown:
		m.moveChoice(m.choiceRows(len(matches)), len(matches))
		return m, nil
	case tea.KeyHome:
		m.moveChoice(-len(matches), len(matches))
		return m, nil
	case tea.KeyEnd:
		m.moveChoice(len(matches), len(matches))
		return m, nil
	case tea.KeyBackspace:
		if nav.filter != "" {
			_, size := utf8.DecodeLastRuneInString(nav.filter)
			m.setChoiceFilter(nav.filter[:len(nav.filter)-size])
		}
		return m, nil
	case tea.KeyRunes, tea.KeySpace:
		if msg.Alt {
			return m, nil
		}
		typed := string(msg.Runes)
		if msg.Type == tea.KeySpace {
			typed = " "
		}
		// With nothing typed yet, a digit is the old direct pick. Once a filter
		// has started, digits are part of it: model ids are full of them.
		if nav.filter == "" && len(typed) == 1 && typed[0] >= '1' && typed[0] <= '9' {
			if n := int(typed[0] - '0'); n <= len(m.choiceItems) {
				m.pickChoice(n - 1)
			}
			return m, nil
		}
		if nav.filter == "" {
			typed = strings.TrimLeft(typed, " ")
		}
		if typed != "" {
			m.setChoiceFilter(nav.filter + typed)
		}
		return m, nil
	}
	return m, nil
}

// pickChoice closes the picker and applies choiceItems[i].
func (m *Model) pickChoice(i int) {
	item := m.choiceItems[i]
	m.closeChoice()
	m.appendLine(bannerStyle.Render("  → " + item.apply()))
}

// setChoiceFilter replaces the filter and puts the highlight back on the first
// match: the row it was on may no longer be in the list.
func (m *Model) setChoiceFilter(f string) {
	m.choiceNav = choiceNav{filter: f}
}

// moveChoice moves the highlight by delta rows, stopping at either end.
func (m *Model) moveChoice(delta, n int) {
	c := m.choiceNav.cursor + delta
	if c >= n {
		c = n - 1
	}
	if c < 0 {
		c = 0
	}
	m.choiceNav.cursor = c
}

// choiceRows is how many item rows the window shows for n matches: whatever
// the terminal has left after the picker's title and footer and the status
// bar, capped at maxPickerRows, and never more rows than there are matches
// (one row still, for "no matches"). clampBottom trims an over-tall live
// region from the top, which would cut the title and the first rows, so the
// window is sized here rather than left to it.
func (m *Model) choiceRows(n int) int {
	fixed := m.liveLines(caption(askStyle.Render(m.choicePrompt))) + 1 + // title, footer
		m.liveLines(caption(m.statusLine()))
	rows := m.height - 1 - fixed
	if rows > maxPickerRows {
		rows = maxPickerRows
	}
	if rows > n {
		rows = n
	}
	if rows < 1 {
		rows = 1
	}
	return rows
}

// liveLines is how many rows s occupies once fitLiveRegion has wrapped it.
func (m *Model) liveLines(s string) int {
	return strings.Count(m.fitLiveRegion(s), "\n") + 1
}

// choiceView renders the open picker for the live region: the title, a window
// of rows with the highlighted one marked, and a footer carrying the filter,
// the position and the keys.
func (m *Model) choiceView() string {
	nav := &m.choiceNav
	matches := m.choiceMatches()
	if nav.cursor >= len(matches) {
		nav.cursor = max(len(matches)-1, 0)
	}
	rows := m.choiceRows(len(matches))
	// Scroll the window just far enough to keep the highlight in it.
	if nav.cursor < nav.offset {
		nav.offset = nav.cursor
	}
	if nav.cursor >= nav.offset+rows {
		nav.offset = nav.cursor - rows + 1
	}
	if nav.offset > len(matches)-rows {
		nav.offset = max(len(matches)-rows, 0)
	}

	// Rows are truncated, not wrapped: a wrapped row is two terminal rows the
	// window did not budget for. The label's end is the least useful part
	// (a context size, "(current)").
	width := m.width - 2
	fit := func(s string) string {
		if width < 10 {
			return s
		}
		return ansi.Truncate(s, width, "…")
	}

	lines := []string{caption(askStyle.Render(m.choicePrompt))}
	if len(matches) == 0 {
		lines = append(lines, fit(caption(hintStyle.Render("  no matches — backspace to edit the filter"))))
	}
	for r := nav.offset; r < nav.offset+rows && r < len(matches); r++ {
		idx := matches[r]
		num := "   "
		if nav.filter == "" && idx < 9 {
			num = fmt.Sprintf("%d) ", idx+1)
		}
		if r == nav.cursor {
			lines = append(lines, fit(caption(suggestStyle.Render("› "+num+m.choiceItems[idx].label))))
		} else {
			lines = append(lines, fit(caption(toolStyle.Render("  "+num+m.choiceItems[idx].label))))
		}
	}

	var footer string
	if nav.filter != "" {
		footer = askStyle.Render("filter: "+nav.filter) +
			hintStyle.Render(fmt.Sprintf("  %d of %d · ↑/↓ · enter select · esc cancel",
				len(matches), len(m.choiceItems)))
	} else {
		pos := ""
		if len(matches) > rows {
			pos = fmt.Sprintf("%d/%d · ", nav.cursor+1, len(matches))
		}
		footer = hintStyle.Render(fmt.Sprintf("%s↑/↓ · enter select · 1-%d · type to filter · esc cancel",
			pos, min(len(m.choiceItems), 9)))
	}
	lines = append(lines, fit(caption(footer)))
	return strings.Join(lines, "\n")
}
