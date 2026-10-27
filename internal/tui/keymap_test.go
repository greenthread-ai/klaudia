package tui

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Every binding in the keymap is documented, and nothing is documented that is
// not a binding.
//
// The registry is walked by reflection rather than listed here, so a new field
// on keymap fails this test until helpOrder places it in the help. That is the
// whole point of the change: /help used to be a hand-written const sitting
// beside the code that matched the keys, and adding a binding without updating
// it broke nothing.
func TestHelpDocumentsEveryBinding(t *testing.T) {
	m := newTestModel()
	km := m.keymap()
	help := m.keyHints()

	v := reflect.ValueOf(km)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		b, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			t.Fatalf("keymap field %s is not a key.Binding", name)
		}
		h := b.Help()
		if h.Key == "" || h.Desc == "" {
			t.Errorf("binding %s has no help text, so it cannot appear in /help", name)
			continue
		}
		if !strings.Contains(help, chordCase(h.Key)) {
			t.Errorf("binding %s (%s) is missing from /help:\n%s", name, h.Key, help)
		}
		if !strings.Contains(help, capitalise(h.Desc)) {
			t.Errorf("binding %s's description is missing from /help:\n%s", name, help)
		}
	}

	// The editing chords the textarea owns are documented from its keymap, not
	// restated, so the help cannot claim one the widget does not bind.
	for _, b := range []key.Binding{m.input.KeyMap.DeleteBeforeCursor, m.input.KeyMap.DeleteAfterCursor} {
		if !strings.Contains(help, chordCase(b.Help().Key)) {
			t.Errorf("textarea binding %q is missing from /help:\n%s", b.Help().Key, help)
		}
	}
}

// The Return chords depend on config, so the help has to as well. With
// input.enter = "newline" the old const still said Enter sends, and so did the
// intro banner — on every start, for exactly the users who had configured it
// not to.
func TestHelpFollowsTheConfiguredReturnMode(t *testing.T) {
	for _, tt := range []struct {
		name         string
		enterInserts bool
		sends        string
	}{
		{"enter sends", false, "Enter"},
		{"enter inserts a newline", true, altReturnLabel()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel()
			m.sess.EnterInserts = tt.enterInserts
			km := m.keymap()

			// The binding, the help and the intro all agree.
			if got := chordCase(km.Submit.Help().Key); got != tt.sends {
				t.Errorf("Submit help key = %q, want %q", got, tt.sends)
			}
			line := helpLineFor(t, m.keyHints(), "Send the prompt")
			if !strings.Contains(line, tt.sends) {
				t.Errorf("/help send row = %q, want it to name %q", line, tt.sends)
			}
			banner := intro("m", "", "t", nil, km.Submit)
			if !strings.Contains(stripANSI(banner), tt.sends+" sends") {
				t.Errorf("intro does not tell the user to press %q:\n%s", tt.sends, stripANSI(banner))
			}
			for _, ln := range strings.Split(stripANSI(banner), "\n") {
				if w := ansi.StringWidth(ln); strings.Contains(ln, " sends ·") && w >= 80 {
					t.Errorf("intro line is %d columns, want < 80 so it does not wrap: %q", w, ln)
				}
			}

			// And the key actually does it.
			send := tea.KeyPressMsg{Code: tea.KeyEnter}
			if tt.enterInserts {
				send = tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
			}
			if km.action(send) != actionSubmit {
				t.Errorf("the documented send chord does not submit")
			}
		})
	}
}

// The textarea must not keep a newline chord that now sends: it sees keys the
// prompt does not consume, and a widget that inserts a newline for the send key
// would both send and insert.
func TestTextareaNewlineChordTracksTheConfig(t *testing.T) {
	for _, enterInserts := range []bool{false, true} {
		km := newKeymap(enterInserts)
		in := newPromptInput()
		in.KeyMap.InsertNewline = km.Newline
		for _, k := range km.Submit.Keys() {
			for _, nk := range in.KeyMap.InsertNewline.Keys() {
				if k == nk {
					t.Errorf("enterInserts=%v: %q both sends and inserts a newline", enterInserts, k)
				}
			}
		}
	}
}

// helpLineFor returns the rendered help row containing desc.
func helpLineFor(t *testing.T, help, desc string) string {
	t.Helper()
	for _, line := range strings.Split(help, "\n") {
		if strings.Contains(line, desc) {
			return line
		}
	}
	t.Fatalf("no help row describes %q:\n%s", desc, help)
	return ""
}

// The newline chord is labelled as the keyboard labels it.
func TestAltReturnLabelFollowsThePlatform(t *testing.T) {
	defer func(g string) { goos = g }(goos)
	goos = "darwin"
	if got := altReturnLabel(); got != "Option+Return" {
		t.Errorf("darwin: %q", got)
	}
	goos = "linux"
	if got := altReturnLabel(); got != "Alt+Enter" {
		t.Errorf("linux: %q", got)
	}
	if strings.Contains(stripANSI(newTestModel().keyHints()), "Ctrl+J") {
		t.Error("help should not advertise Ctrl+J")
	}
}

// A trailing backslash makes Return a newline, in any terminal; a backslash
// anywhere else is just text.
func TestBackslashReturnInsertsANewline(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	typeRunes(m, `first\`)
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.input.Value(); got != "first\n" {
		t.Fatalf("after \\ + Return: %q, want %q", got, "first\n")
	}
	if m.state != stateIdle {
		t.Fatalf("state = %v; the prompt must not have been sent", m.state)
	}
	typeRunes(m, "second")
	if got := m.input.Value(); got != "first\nsecond" {
		t.Errorf("continued input = %q", got)
	}

	// Mid-line it is part of what is being written.
	m2 := newTestModel()
	m2.resize(100, 30)
	typeRunes(m2, `a\b`)
	m2.onKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	m2.onKey(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m2.backslashContinues() {
		t.Error("a backslash before the cursor but not at the end must not continue the line")
	}
}

// Shift+Enter is bound in both modes but only named once the terminal has
// said it can send it: naming it on Apple Terminal would offer a chord that
// sends the prompt.
func TestShiftEnterNamedOnlyWhenTheTerminalReportsIt(t *testing.T) {
	defer func(v bool) { keysDisambiguated = v }(keysDisambiguated)
	defer func(v string) { goos = v }(goos)
	goos = "linux"
	shift := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}
	for _, enterInserts := range []bool{false, true} {
		if got := keyAction(shift, enterInserts); got != actionNewline {
			t.Errorf("enterInserts=%v: Shift+Enter = %v, want newline", enterInserts, got)
		}
	}
	ctrl := tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl}
	if got := keyAction(ctrl, true); got != actionSubmit {
		t.Errorf("newline mode: Ctrl+Enter = %v, want submit", got)
	}
	keysDisambiguated = false
	if h := newKeymap(false).Newline.Help().Key; h != "Alt+Enter" {
		t.Errorf("unreported: newline labelled %q, want Alt+Enter", h)
	}
	keysDisambiguated = true
	if h := newKeymap(false).Newline.Help().Key; h != "Shift+Enter" {
		t.Errorf("reported: newline labelled %q, want Shift+Enter", h)
	}
	if h := newKeymap(true).Submit.Help().Key; h != "Ctrl+Enter" {
		t.Errorf("reported, newline mode: send labelled %q, want Ctrl+Enter", h)
	}
}
