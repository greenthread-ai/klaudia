package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeRunes feeds each rune as its own key press, the way real typing arrives —
// the bug only shows up on incremental growth, not a single bulk insertion.
func typeRunes(m *Model, s string) {
	for _, r := range s {
		m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		// Render between keystrokes as the real frame loop does: textarea.View
		// populates the (pointer-shared) viewport's line buffer, which
		// repositionView needs on the next key to scroll at all.
		_ = m.input.View()
	}
}

// A line long enough to soft-wrap must grow the box AND keep its first row
// visible. The regression: bubbles' repositionView ran at the pre-growth height
// and scrolled the view down to chase the cursor onto the new wrapped row, then
// SetHeight grew the box without pulling the view back up — so the top row
// vanished and a blank row appeared at the bottom.
func TestInputWrapKeepsTopRowVisible(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.state = stateIdle
	m.width = 25 // inputWidth() = 20, so the line wraps every ~20 columns
	m.input.SetWidth(m.inputWidth())

	// ~3 wrapped rows (≤ MaxHeight 6), one unbroken word so it hard-wraps.
	typeRunes(m, "START"+strings.Repeat("x", 45)+"END")

	view := stripANSI(m.input.View())
	if !strings.Contains(view, "START") {
		t.Errorf("top of the wrapped line scrolled out of view:\n%s", view)
	}
	if !strings.Contains(view, "END") {
		t.Errorf("the cursor line (end of input) is not visible:\n%s", view)
	}
}

// Past the 6-row cap the box stops growing and follows the cursor instead, so
// the newest text stays visible even though the oldest scrolls off. This must
// keep working — the fix must not pin the view to the top for overflowing input.
func TestInputOverflowFollowsCursor(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.state = stateIdle
	m.width = 25
	m.input.SetWidth(m.inputWidth())

	// Well past 6 rows of 20 columns.
	typeRunes(m, strings.Repeat("a", 200)+"TAIL")

	view := stripANSI(m.input.View())
	if !strings.Contains(view, "TAIL") {
		t.Errorf("the cursor line scrolled out of view on overflow:\n%s", view)
	}
	// The box is capped: it renders MaxHeight rows, not 200/20 rows.
	if got := strings.Count(view, "\n") + 1; got > m.input.MaxHeight {
		t.Errorf("box rendered %d rows, want ≤ MaxHeight %d", got, m.input.MaxHeight)
	}
}
