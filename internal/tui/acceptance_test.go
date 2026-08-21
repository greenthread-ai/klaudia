package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The five acceptance criteria Klaudia's terminal UX is held to. Each of these
// reproduced a real failure before the P0/P1 work; they are here so a
// regression shows up as a named criterion rather than as a subtle behaviour
// change somewhere in tui.go.
//
//  1. I can copy anything I can see.
//  2. I can paste anything a terminal app should accept.
//  3. I can page up and inspect old content without fighting the application.
//  4. New output never steals my reading position after I scroll away.
//  5. I can always understand how to get back to the newest output.

// Criterion 1 — what is on screen is what lands in the clipboard: no margins,
// no width padding, no expanded tabs, no line numbers.
func TestCriterion1_CopyAnythingVisible(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)

	m.appendMarkdown("Try:\n\n```go\nfunc f() {\n\tif x {\n\t\treturn 1\n\t}\n}\n```\n")
	m.renderEvent(mkResult("Bash", "NAME\tSTATUS\npod\tRunning", false))

	out := visibleText(m.transcript.String())
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasSuffix(ln, " ") {
			t.Errorf("padded line copies with trailing spaces: %q", ln)
		}
	}
	for _, want := range []string{"func f() {", "\tif x {", "\t\treturn 1"} {
		if !strings.Contains(out, "\n"+want) {
			t.Errorf("code must copy as source; missing %q", want)
		}
	}
	if !strings.Contains(out, "NAME\tSTATUS") {
		t.Error("tabular tool output must keep its tabs")
	}
}

// Criterion 2 — the paste torture list, end to end through the real key path.
func TestCriterion2_PasteAnythingReasonable(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"one line", "a single line"},
		{"multiline", "line one\nline two\nline three"},
		{"CRLF", "one\r\ntwo\r\nthree"},
		{"tabs", "func main() {\n\tprintln(1)\n}"},
		{"unicode", "🎉 éàü 日本語 ┌─┐"},
		{"stack trace", strings.Repeat("  at frame (file.js:12:3)\n", 40)},
		{"1000 log lines", strings.Repeat("2024-01-01 INFO something happened\n", 1000)},
		{"mixed tabs and CRLF", "a\tb\r\nc\td"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			m.resize(80, 24)
			m = paste(m, tc.text)

			want := normalizeNewlines(tc.text)
			if got := m.promptValue(); got != want {
				t.Fatalf("paste round-trip lost data:\n got %d bytes\nwant %d bytes", len(got), len(want))
			}
			// A paste must never submit on its own.
			if m.state != stateIdle {
				t.Errorf("pasting changed state to %v; nothing should be sent without Enter", m.state)
			}
			// Nor should it make the input box unusable.
			if rows := wrappedRowCount(m.input.Value(), m.input.Width()); rows > m.input.MaxHeight {
				t.Errorf("input grew to %d rows, past the %d-row cap", rows, m.input.MaxHeight)
			}
		})
	}
}

// Criteria 3, 4 and 5 hold structurally: Klaudia prints into the terminal's own
// scrollback and never moves it, so there is nothing to fight, nothing to steal
// a reading position, and no app-specific way back to the bottom to learn.
// These assert the properties that guarantee that, since they are the ones a
// future change could quietly break.
func TestCriteria345_TerminalOwnsScrollback(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)

	// (3) No scrollback key is intercepted — they all reach the terminal.
	for _, k := range []tea.KeyType{tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd} {
		before := m.transcript.Len()
		model, cmd := m.onKey(tea.KeyMsg{Type: k})
		m = model.(*Model)
		if cmd != nil {
			t.Errorf("key %v produced a command; scrolling must be the terminal's job", k)
		}
		if m.transcript.Len() != before {
			t.Errorf("key %v changed the transcript", k)
		}
	}

	// (4) Streaming output only ever appends. There is no viewport position to
	// steal, and committed output is never redrawn.
	m.appendLine("old content the user is reading")
	m.out.drainText()
	for i := 0; i < 40; i++ {
		m.appendText(fmt.Sprintf("streamed paragraph %d.\n\n", i))
	}
	if strings.Contains(m.View(), "old content the user is reading") {
		t.Error("committed output must never be redrawn into the live region")
	}

	// (5) The live region is always the newest thing on screen and always fits,
	// so "the bottom" is unambiguous and the status bar is never pushed off.
	view := m.View()
	if n := len(strings.Split(view, "\n")); n >= m.height {
		t.Errorf("live region is %d lines on a %d-line terminal; it must leave room", n, m.height)
	}
	if !strings.Contains(view, "turns") {
		t.Error("the status bar must stay visible as the anchor for 'newest output'")
	}
}

// A long command must not destroy the session, and must stay reachable.
func TestLongCommandOutputStaysUsableAndRecoverable(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	huge := strings.Repeat("compiling module/path/file.go\n", 5000)
	m.renderEvent(mkResult("Bash", huge, false))

	printed := visibleText(m.transcript.String())
	if n := strings.Count(printed, "\n"); n > 20 {
		t.Errorf("a 5000-line result printed %d lines inline; it should be previewed", n)
	}
	if !strings.Contains(printed, "/last") {
		t.Error("the preview must say how to get the full output")
	}
	res, ok := m.results.latest()
	if !ok || res.content != huge {
		t.Error("the full output must be recoverable byte-for-byte")
	}
}

// Ctrl+C is the one key whose semantics people have muscle memory for.
func TestInterruptIsAlwaysSafe(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.setState(stateRunning)
	m.turnCancel = func() {}
	m.input.SetValue("a draft I care about")

	// Mid-turn: interrupts, does not quit, does not lose the draft.
	m, cmd := ctrlC(m)
	if isQuit(cmd) {
		t.Fatal("Ctrl+C mid-turn must not quit")
	}
	if m.input.Value() != "a draft I care about" {
		t.Error("interrupting a turn must not discard the draft")
	}
}
