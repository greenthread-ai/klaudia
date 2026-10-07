package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pickerKey sends one key to the model and returns it.
func pickerKey(m *Model, msg tea.KeyMsg) *Model {
	model, _ := m.Update(msg)
	return model.(*Model)
}

func pickerRunes(m *Model, s string) *Model {
	for _, r := range s {
		if r == ' ' {
			m = pickerKey(m, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
			continue
		}
		m = pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// openPicker opens a picker of n items labelled "item-01".. on a w×h terminal;
// picked records the label of the item applied.
func openPicker(t *testing.T, n, w, h int) (*Model, *string) {
	t.Helper()
	m := newTestModel()
	m.resize(w, h)
	picked := new(string)
	items := make([]choiceItem, n)
	for i := range items {
		label := fmt.Sprintf("item-%02d", i+1)
		items[i] = choiceItem{label: label, apply: func() string { *picked = label; return "picked " + label }}
	}
	m.startChoice("Pick one:", items)
	return m, picked
}

func TestPickerArrowsAndEnter(t *testing.T) {
	m, picked := openPicker(t, 5, 100, 40)
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyUp})
	if !strings.Contains(visibleText(m.bottomView()), "› 2) item-02") {
		t.Errorf("highlight not on the second row:\n%s", visibleText(m.bottomView()))
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "item-02" {
		t.Fatalf("Enter picked %q, want item-02", *picked)
	}
	if m.state != stateIdle || m.choiceItems != nil {
		t.Errorf("picker not closed: state=%v items=%d", m.state, len(m.choiceItems))
	}
	if !strings.Contains(visibleText(m.transcript.String()), "picked item-02") {
		t.Error("the choice's confirmation was not recorded in scrollback")
	}
}

// The highlight stops at either end rather than wrapping or running off.
func TestPickerCursorClamps(t *testing.T) {
	m, picked := openPicker(t, 3, 100, 40)
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyUp})
	for i := 0; i < 10; i++ {
		m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "item-03" {
		t.Fatalf("picked %q, want the last item", *picked)
	}
}

// Digits keep picking directly while no filter is typed.
func TestPickerDigitPicksDirectly(t *testing.T) {
	m, picked := openPicker(t, 12, 100, 40)
	m = pickerRunes(m, "4")
	if *picked != "item-04" {
		t.Fatalf("digit 4 picked %q", *picked)
	}

	m, picked = openPicker(t, 3, 100, 40)
	m = pickerRunes(m, "7") // out of range: ignored, picker stays open
	if *picked != "" || m.state != stateAwaitingChoice {
		t.Fatalf("an out-of-range digit acted: picked=%q state=%v", *picked, m.state)
	}
}

// Typing filters incrementally; digits are part of a filter once one has
// started; Backspace edits it; Enter picks the highlighted match.
func TestPickerTypeToFilter(t *testing.T) {
	m, picked := openPicker(t, 25, 100, 40)
	m = pickerRunes(m, "item-2")
	if got := len(m.choiceMatches()); got != 6 { // item-20..item-25
		t.Fatalf("filter item-2 kept %d items, want 6", got)
	}
	m = pickerRunes(m, "3")
	if got := m.choiceMatches(); len(got) != 1 || m.choiceItems[got[0]].label != "item-23" {
		t.Fatalf("filter item-23 kept %v", got)
	}
	if *picked != "" {
		t.Fatalf("a digit typed into a filter picked %q", *picked)
	}
	view := visibleText(m.bottomView())
	if !strings.Contains(view, "filter: item-23") || !strings.Contains(view, "1 of 25") {
		t.Errorf("filter not shown:\n%s", view)
	}

	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if m.choiceNav.filter != "item-2" {
		t.Fatalf("Backspace left filter %q", m.choiceNav.filter)
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "item-21" {
		t.Fatalf("picked %q, want the second match item-21", *picked)
	}
}

// Each word of the filter must match, in any order, ignoring case.
func TestPickerFilterWords(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)
	m.startChoice("Select a model:", []choiceItem{
		{label: "Claude Opus 4.5  claude-opus-4-5"},
		{label: "Claude Sonnet 4.5  claude-sonnet-4-5"},
		{label: "Claude Opus 5  claude-opus-5"},
	})
	m = pickerRunes(m, "OPUS 4")
	if got := m.choiceMatches(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("matches = %v, want [0]", got)
	}
}

func TestPickerNoMatches(t *testing.T) {
	m, picked := openPicker(t, 5, 100, 40)
	m = pickerRunes(m, "zzz")
	if !strings.Contains(visibleText(m.bottomView()), "no matches") {
		t.Errorf("an empty filter result should say so:\n%s", visibleText(m.bottomView()))
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "" || m.state != stateAwaitingChoice {
		t.Fatalf("Enter with no matches acted: picked=%q state=%v", *picked, m.state)
	}
}

func TestPickerEscCancels(t *testing.T) {
	m, picked := openPicker(t, 5, 100, 40)
	m = pickerRunes(m, "item")
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if *picked != "" || m.state != stateIdle || m.choiceItems != nil {
		t.Fatalf("Esc: picked=%q state=%v", *picked, m.state)
	}
	if m.choiceNav != (choiceNav{}) {
		t.Errorf("filter state survived the picker: %+v", m.choiceNav)
	}
}

// A list taller than the terminal shows a window that follows the highlight,
// and the whole live region still fits: clampBottom would otherwise cut the
// title and the top rows off.
func TestPickerScrollsWithinTerminal(t *testing.T) {
	const h = 12
	m, picked := openPicker(t, 40, 80, h)
	view := visibleText(m.View())
	if n := len(strings.Split(view, "\n")); n > h-1 {
		t.Fatalf("live region is %d lines on a %d-line terminal:\n%s", n, h, view)
	}
	if !strings.Contains(view, "Pick one:") || !strings.Contains(view, "item-01") {
		t.Errorf("title or first row missing:\n%s", view)
	}
	if strings.Contains(view, "item-40") {
		t.Errorf("the whole list was drawn on a short terminal:\n%s", view)
	}

	for i := 0; i < 29; i++ {
		m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	view = visibleText(m.View())
	if !strings.Contains(view, "› ") || !strings.Contains(view, "item-30") || strings.Contains(view, "item-01") {
		t.Errorf("window did not follow the highlight to item-30:\n%s", view)
	}
	if !strings.Contains(view, "30/40") {
		t.Errorf("position not shown:\n%s", view)
	}
	if n := len(strings.Split(view, "\n")); n > h-1 {
		t.Fatalf("scrolled live region is %d lines on a %d-line terminal", n, h)
	}

	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "item-40" {
		t.Fatalf("End then Enter picked %q", *picked)
	}
}

// On a tall terminal the window stops at maxPickerRows.
func TestPickerWindowCapped(t *testing.T) {
	m, _ := openPicker(t, 40, 100, 80)
	view := visibleText(m.bottomView())
	rows := strings.Count(view, "item-")
	if rows != maxPickerRows {
		t.Fatalf("window shows %d rows, want %d", rows, maxPickerRows)
	}
}
