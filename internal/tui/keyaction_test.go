package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyActionDefaultMode(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want inputAction
	}{
		{"return submits", tea.KeyMsg{Type: tea.KeyEnter}, actionSubmit},
		{"ctrl+j inserts", tea.KeyMsg{Type: tea.KeyCtrlJ}, actionNewline},
		{"alt+return inserts", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, actionNewline},
		{"other keys unaffected", tea.KeyMsg{Type: tea.KeyCtrlR}, actionNone},
	}
	for _, tc := range cases {
		if got := keyAction(tc.msg, false); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestKeyActionNewlineMode(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want inputAction
	}{
		{"return inserts", tea.KeyMsg{Type: tea.KeyEnter}, actionNewline},
		{"alt+return submits", tea.KeyMsg{Type: tea.KeyEnter, Alt: true}, actionSubmit},
		// The escape hatch: ctrl+j needs no terminal support, so a user who
		// cannot produce alt+Return can still send.
		{"ctrl+j submits", tea.KeyMsg{Type: tea.KeyCtrlJ}, actionSubmit},
	}
	for _, tc := range cases {
		if got := keyAction(tc.msg, true); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestEnterInsertsSetting(t *testing.T) {
	var warned string
	warn := func(m string) { warned = m }

	for _, s := range []string{"", "send"} {
		if EnterInserts(s, warn) {
			t.Errorf("%q should keep Return sending", s)
		}
	}
	if !EnterInserts("newline", warn) {
		t.Error(`"newline" should make Return insert`)
	}
	if warned != "" {
		t.Fatalf("valid settings warned: %q", warned)
	}

	// A typo must not strand the user in a prompt that cannot send.
	if EnterInserts("newlnie", warn) {
		t.Error("unknown setting should fall back to sending")
	}
	if warned == "" {
		t.Error("unknown setting should warn")
	}
}

// The two paths that read Return — the idle prompt and the queue-while-running
// path — must agree about which chord sends. They did not in the first cut of
// this change: the running path still keyed off tea.KeyEnter directly, so in
// "newline" mode a Return meant to add a line queued a follow-up instead.
func TestNewlineModeAgreesAcrossStates(t *testing.T) {
	m := newTestModel()
	m.sess.EnterInserts = true

	m.state = stateIdle
	m.input.SetValue("line one")
	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.input.Value(); got != "line one\n" {
		t.Fatalf("idle: Return should insert a newline, input = %q", got)
	}

	m.state = stateRunning
	m.input.SetValue("follow up")
	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.input.Value(); got != "follow up\n" {
		t.Fatalf("running: Return should insert a newline, input = %q", got)
	}
	if m.steer.pending() {
		t.Fatal("running: Return must not queue a follow-up in newline mode")
	}

	// alt+Return is what sends, and while running that means queueing.
	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if !m.steer.pending() {
		t.Fatal("running: alt+Return should queue the follow-up")
	}
}

// Default mode keeps queueing on Return, and alt+Return now adds a line
// instead of sending — the one behaviour this change takes away.
func TestDefaultModeQueuesOnReturn(t *testing.T) {
	m := newTestModel()
	m.state = stateRunning
	m.input.SetValue("follow up")

	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.steer.pending() {
		t.Fatal("Return should queue while running")
	}

	m.input.SetValue("more")
	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if got := m.input.Value(); got != "more\n" {
		t.Fatalf("alt+Return should insert a newline, input = %q", got)
	}
}
