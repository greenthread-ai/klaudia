package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// pickerView renders the live region the picker occupies, with the styling
// removed. The picker is deliberately not in the transcript, so a test that
// reads scrollback sees nothing.
func pickerView(m *Model) string { return visibleText(m.bottomView()) }

// pickerRows counts the option rows on screen, which is the window size — the
// title, the hint and the status caption are not rows.
func pickerRows(m *Model) int {
	n := 0
	for _, ln := range strings.Split(pickerView(m), "\n") {
		if strings.Contains(ln, "model-") {
			n++
		}
	}
	return n
}

// choiceModel opens a picker over n numbered options, recording which one was
// applied.
func choiceModel(t *testing.T, labels ...string) (*Model, *string) {
	t.Helper()
	m := newTestModel()
	m.resize(80, 24)
	picked := new(string)
	items := make([]choiceItem, 0, len(labels))
	for _, label := range labels {
		label := label
		items = append(items, choiceItem{label: label, apply: func() string {
			*picked = label
			return "chose " + label
		}})
	}
	m.startChoice("Pick one:", items)
	return m, picked
}

// The picker used to print its list into scrollback and wait for a digit.
// Scrollback cannot be revised, so a list that filters has to live in the
// region that is redrawn — the same place the @ menu and the queued-message
// hint already live.
func TestPickerRendersLiveNotIntoScrollback(t *testing.T) {
	m, _ := choiceModel(t, "alpha", "beta")

	if out := visibleText(m.transcript.String()); strings.Contains(out, "alpha") {
		t.Errorf("the picker wrote a frozen copy into scrollback:\n%s", out)
	}
	view := pickerView(m)
	for _, want := range []string{"Pick one:", "1) alpha", "2) beta", "esc to cancel"} {
		if !strings.Contains(view, want) {
			t.Errorf("live region missing %q:\n%s", want, view)
		}
	}
}

func TestPickerArrowsAndEnterSelect(t *testing.T) {
	m, picked := choiceModel(t, "alpha", "beta", "gamma")

	m.onKey(tea.KeyMsg{Type: tea.KeyDown})
	if !strings.Contains(pickerView(m), "❯ 2) beta") {
		t.Errorf("down should move the selection:\n%s", pickerView(m))
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})

	if *picked != "beta" {
		t.Errorf("enter applied %q, want beta", *picked)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle after a selection", m.state)
	}
	if !strings.Contains(visibleText(m.transcript.String()), "chose beta") {
		t.Error("the result line belongs in scrollback — it is still true afterwards")
	}
}

// Wrapping, so Up from the top reaches the last entry without a long hold.
func TestPickerSelectionWraps(t *testing.T) {
	m, picked := choiceModel(t, "alpha", "beta", "gamma")
	m.onKey(tea.KeyMsg{Type: tea.KeyUp})
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "gamma" {
		t.Errorf("up from the first entry applied %q, want gamma", *picked)
	}
}

func TestPickerFiltersAsYouType(t *testing.T) {
	m, picked := choiceModel(t, "Claude Opus 5", "Claude Sonnet 5", "Claude Haiku 4.5")

	typeInto(m, "son")
	if len(m.choiceHits) != 1 {
		t.Fatalf("filter %q matched %d entries, want 1:\n%s", m.choiceFilter, len(m.choiceHits), pickerView(m))
	}
	view := pickerView(m)
	if !strings.Contains(view, "Sonnet") || strings.Contains(view, "Haiku") {
		t.Errorf("the view should show the matches and nothing else:\n%s", view)
	}
	if !strings.Contains(view, "son") {
		t.Errorf("the filter must be visible, or there is no way to know why the list shrank:\n%s", view)
	}

	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "Claude Sonnet 5" {
		t.Errorf("enter applied %q, want the filtered entry", *picked)
	}
}

// Backspace has to widen the filter rather than cancelling, or a typo is a
// dead end.
func TestPickerBackspaceWidensTheFilter(t *testing.T) {
	m, _ := choiceModel(t, "alpha", "beta")

	typeInto(m, "alz")
	if len(m.choiceHits) != 0 {
		t.Fatalf("%q should match nothing, got %d", m.choiceFilter, len(m.choiceHits))
	}
	if !strings.Contains(pickerView(m), "no match") {
		t.Errorf("an empty list must say why it is empty:\n%s", pickerView(m))
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.choiceFilter != "al" || len(m.choiceHits) != 1 {
		t.Errorf("filter = %q with %d hits, want \"al\" and alpha", m.choiceFilter, len(m.choiceHits))
	}
	if m.state != stateAwaitingChoice {
		t.Error("backspacing the filter closed the picker")
	}
}

// Digits cannot both pick a row and filter: "claude-opus-5" is full of them.
// They pick while the filter is empty and are typed once it is not, and the
// numbers shown alongside the rows follow the same rule — the mapping is
// visible exactly when it is live.
func TestDigitsPickUntilAFilterTakesThem(t *testing.T) {
	m, picked := choiceModel(t, "alpha", "beta", "gamma")
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if *picked != "gamma" {
		t.Errorf("a digit on an unfiltered list applied %q, want gamma", *picked)
	}

	m, picked = choiceModel(t, "gpt-5", "gpt-4", "claude-3")
	typeInto(m, "gpt")
	if !strings.Contains(pickerView(m), "gpt-5") || strings.Contains(pickerView(m), "1) gpt-5") {
		t.Errorf("a filtered list must drop the digit column:\n%s", pickerView(m))
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	if *picked != "" {
		t.Fatalf("a digit typed into a filter picked %q", *picked)
	}
	if m.choiceFilter != "gpt4" {
		t.Errorf("filter = %q, want the digit appended", m.choiceFilter)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "gpt-4" {
		t.Errorf("applied %q, want gpt-4", *picked)
	}
}

// A list longer than the window scrolls with the selection and says where in
// it you are, rather than being cut to what fits.
func TestPickerWindowsALongListWithACounter(t *testing.T) {
	labels := make([]string, 30)
	for i := range labels {
		labels[i] = "model-" + strconv.Itoa(i)
	}
	m, picked := choiceModel(t, labels...)

	if rows := pickerRows(m); rows != choiceRows {
		t.Errorf("window shows %d rows, want %d:\n%s", rows, choiceRows, pickerView(m))
	}
	if !strings.Contains(pickerView(m), "1/30") {
		t.Errorf("a windowed list must say how long it is:\n%s", pickerView(m))
	}

	for i := 0; i < 12; i++ {
		m.onKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	view := pickerView(m)
	if !strings.Contains(view, "❯ model-12") {
		t.Errorf("the selection walked off the window:\n%s", view)
	}
	if !strings.Contains(view, "13/30") {
		t.Errorf("counter should track the selection:\n%s", view)
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if *picked != "model-12" {
		t.Errorf("applied %q, want model-12", *picked)
	}
}

// A short terminal shows a shorter list. The live region is clamped to the
// bottom of the screen, so a window taller than the terminal loses its top
// rows — including, at the start, the selected one.
func TestPickerShrinksOnAShortTerminal(t *testing.T) {
	labels := make([]string, 30)
	for i := range labels {
		labels[i] = "model-" + strconv.Itoa(i)
	}
	m, _ := choiceModel(t, labels...)
	m.resize(80, 9)

	if rows := pickerRows(m); rows >= choiceRows {
		t.Errorf("%d rows on a 9-line terminal:\n%s", rows, pickerView(m))
	}
	if !strings.Contains(pickerView(m), "❯ ") {
		t.Errorf("the selected row must survive the shrink:\n%s", pickerView(m))
	}
}

func TestPickerEscCancelsWithoutApplying(t *testing.T) {
	m, picked := choiceModel(t, "alpha", "beta")
	typeInto(m, "bet")
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc})

	if *picked != "" {
		t.Errorf("esc applied %q", *picked)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle", m.state)
	}
	if m.choiceItems != nil || m.choiceHits != nil || m.choiceFilter != "" {
		t.Error("esc left the picker's state behind")
	}
	if !strings.Contains(visibleText(m.transcript.String()), "cancelled") {
		t.Error("cancelling should say so")
	}
}

// Filtering reorders by match quality, but equally-good matches keep the order
// the caller supplied — model lists arrive newest-first and that is meaningful.
func TestFilterKeepsTheCallersOrderAmongEqualMatches(t *testing.T) {
	m, _ := choiceModel(t, "model-newest", "model-middle", "model-oldest")
	typeInto(m, "model")
	var got []string
	for _, i := range m.choiceHits {
		got = append(got, m.choiceItems[i].label)
	}
	want := []string{"model-newest", "model-middle", "model-oldest"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want the supplied order %v", got, want)
	}
}
