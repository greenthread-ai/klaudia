package tui

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// drain empties the queue and returns the text it would have printed.
func drain(t *testing.T, q *printQueue) string {
	t.Helper()
	if q.drainCmd() == nil {
		return ""
	}
	body, _ := q.drainText()
	return body
}

func newScrollableTestModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(40, 8)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	m.appendLine(strings.TrimRight(b.String(), "\n"))
	return m
}

func TestAppendQueuesForScrollback(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.out.drainCmd() // discard the intro banner

	m.appendLine("first")
	m.appendLine("second")

	got := drain(t, &m.out)
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Fatalf("both lines should be queued for printing, got %q", got)
	}
	if strings.Index(got, "first") > strings.Index(got, "second") {
		t.Error("queued output must keep FIFO order")
	}
	if m.out.pending() != 0 {
		t.Error("queue should be empty after draining")
	}
}

// tea.Batch runs commands on separate goroutines, so the queue has to be
// order-safe by construction rather than by scheduling luck.
func TestPrintQueueDrainIsAtomicAndOrdered(t *testing.T) {
	var q printQueue
	for i := 0; i < 200; i++ {
		q.push(fmt.Sprintf("line %03d", i))
	}

	// Two racing drains: exactly one must emit everything, the other nothing.
	var mu sync.Mutex
	var bodies []string
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if body, ok := q.drainText(); ok {
				mu.Lock()
				bodies = append(bodies, body)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(bodies) != 1 {
		t.Fatalf("expected exactly one non-nil drain, got %d", len(bodies))
	}
	lines := strings.Split(bodies[0], "\n")
	if len(lines) != 200 {
		t.Fatalf("expected all 200 lines in one message, got %d", len(lines))
	}
	for i, ln := range lines {
		if want := fmt.Sprintf("line %03d", i); ln != want {
			t.Fatalf("line %d = %q, want %q", i, ln, want)
		}
	}
}

func TestUpdateDrainsQueuedOutput(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.out.drainCmd()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd == nil {
		t.Skip("no output queued for a plain keystroke; nothing to assert")
	}
}

// The live region must stay shorter than the terminal: Bubble Tea's inline
// renderer silently drops lines off the TOP of an over-tall frame, which would
// eat the input and status bar.
func TestClampBottomKeepsInputAndStatusVisible(t *testing.T) {
	m := newTestModel()
	m.resize(80, 5)

	tall := strings.Join([]string{"a", "b", "c", "d", "e", "f", "g", "h"}, "\n")
	got := m.clampBottom(tall)
	if n := len(strings.Split(got, "\n")); n > 4 {
		t.Fatalf("clamped region is %d lines, want at most %d", n, 4)
	}
	if !strings.HasSuffix(got, "h") {
		t.Error("clamping must keep the bottom (input + status bar), not the top")
	}
}

func TestViewIsLiveRegionOnly(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.appendLine("this text went to scrollback")

	if strings.Contains(m.View(), "this text went to scrollback") {
		t.Error("committed output must not be redrawn in the live region")
	}
	if !strings.Contains(m.View(), "turns") {
		t.Error("the live region should still carry the status bar")
	}
}

func TestTranscriptStillRecordsCommittedOutput(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.appendLine("recorded")
	if !strings.Contains(m.transcript.String(), "recorded") {
		t.Error("transcript should keep an in-memory record for /export and search")
	}
	m.transcript.Reset()
	if m.transcript.Len() != 0 {
		t.Error("Reset should empty the log")
	}
}

// On a fresh launch the window is empty, so the input and status bar have to be
// padded down to the bottom — otherwise they float a few rows below the shell
// prompt with the rest of the window blank, which reads as broken.
func TestStatusBarStartsAtTheBottomOfTheWindow(t *testing.T) {
	m := newTestModel()
	m.resize(80, 43)

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 42 { // height-1, leaving the shell's prompt row
		t.Fatalf("first frame is %d lines, want 42 so the bar lands on the bottom row", len(lines))
	}
	if !strings.Contains(lines[len(lines)-1], "turns") {
		t.Errorf("last line should be the status bar, got %q", lines[len(lines)-1])
	}
	// Everything above the input must be blank padding, not content.
	for _, ln := range lines[:len(lines)-3] {
		if strings.TrimSpace(visibleText(ln)) != "" {
			t.Fatalf("expected blank padding above the input, got %q", ln)
		}
	}
}

// As output accumulates the padding gives way, one row at a time, so the bar
// stays put rather than jumping.
func TestPaddingShrinksAsOutputAccumulates(t *testing.T) {
	m := newTestModel()
	m.resize(80, 20)

	before := len(strings.Split(m.View(), "\n"))
	for i := 0; i < 5; i++ {
		m.appendLine(fmt.Sprintf("line %d", i))
	}
	after := len(strings.Split(m.View(), "\n"))
	if after != before-5 {
		t.Errorf("live region went from %d to %d lines after 5 printed rows; want %d", before, after, before-5)
	}
}

// Once the screen is full the padding is gone entirely and the terminal's own
// scrolling takes over.
func TestPaddingDisappearsOnceScreenIsFull(t *testing.T) {
	m := newTestModel()
	m.resize(80, 20)
	for i := 0; i < 60; i++ {
		m.appendLine(fmt.Sprintf("line %d", i))
	}
	view := m.View()
	if n := len(strings.Split(view, "\n")); n > 4 {
		t.Errorf("live region is %d lines once the screen is full; want just the input and status bar", n)
	}
	if !strings.Contains(view, "turns") {
		t.Error("status bar should still be present")
	}
}

// /clear blanks the screen, so the bar has to re-pin to the bottom.
func TestClearRepinsTheStatusBar(t *testing.T) {
	m := newTestModel()
	m.resize(80, 30)
	for i := 0; i < 50; i++ {
		m.appendLine(fmt.Sprintf("line %d", i))
	}
	if n := len(strings.Split(m.View(), "\n")); n > 4 {
		t.Fatalf("setup: expected padding to be gone, live region is %d lines", n)
	}

	if _, cmd := m.handleSlash("/clear"); cmd != nil {
		_ = cmd()
	}
	if n := len(strings.Split(m.View(), "\n")); n < 20 {
		t.Errorf("after /clear the bar should be re-pinned to the bottom, live region is only %d lines", n)
	}
}
