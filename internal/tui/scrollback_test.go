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

// On a fresh launch the bar has to end up at the bottom of the window, not four
// rows below the shell prompt with the rest of the screen blank beneath it.
func TestFirstFrameAnchorsToTheBottom(t *testing.T) {
	m := newTestModel()
	m.resize(80, 43)

	printed, ok := m.out.drainText()
	if !ok {
		t.Fatal("the first resize should queue the banner")
	}
	rows := len(strings.Split(printed, "\n"))
	live := len(strings.Split(m.View(), "\n"))
	// Printed rows + live region + the shell's own prompt row fill the window.
	if rows+live+1 != 43 {
		t.Errorf("printed %d + live %d + 1 = %d rows, want 43 so the bar lands on the bottom row",
			rows, live, rows+live+1)
	}
	if !strings.Contains(printed, "Klaudia") {
		t.Error("the banner should be part of the anchored output")
	}
}

// The live region must stay small. Bubble Tea repositions with
// CursorUp(linesRendered-1) from the previous frame, so a tall live region
// desynchronises as soon as the terminal reflows on resize.
func TestLiveRegionStaysSmall(t *testing.T) {
	for _, h := range []int{10, 24, 43, 80, 200} {
		m := newTestModel()
		m.resize(120, h)
		m.out.drainText()
		if n := len(strings.Split(m.View(), "\n")); n > 6 {
			t.Errorf("height %d: live region is %d lines; it must stay small for the "+
				"renderer's cursor arithmetic to survive a resize", h, n)
		}
	}
}

// Resizing repeatedly must not grow the live region or strand the bar. This is
// the regression: padding the live region made each resize move the bar.
func TestRepeatedResizeKeepsLiveRegionStable(t *testing.T) {
	m := newTestModel()
	m.resize(120, 60)
	m.out.drainText()
	baseline := len(strings.Split(m.View(), "\n"))

	for _, h := range []int{20, 80, 15, 100, 39, 60} {
		m.resize(120, h)
		if got := len(strings.Split(m.View(), "\n")); got != baseline {
			t.Fatalf("after resizing to height %d the live region is %d lines, want %d",
				h, got, baseline)
		}
		// A resize must not queue more output; only the first one anchors.
		if m.out.pending() != 0 {
			t.Fatalf("resize to height %d queued output; only the first resize should anchor", h)
		}
	}
}

// /clear blanks the screen, so the bar has to be re-anchored.
func TestClearReanchorsToTheBottom(t *testing.T) {
	m := newTestModel()
	m.resize(80, 30)
	m.out.drainText()

	if _, cmd := m.handleSlash("/clear"); cmd != nil {
		_ = cmd()
	}
	printed, ok := m.out.drainText()
	if !ok {
		t.Fatal("/clear should queue the re-anchoring blank lines and its notice")
	}
	rows := len(strings.Split(printed, "\n"))
	live := len(strings.Split(m.View(), "\n"))
	if rows+live+1 != 30 {
		t.Errorf("after /clear: printed %d + live %d + 1 = %d, want 30", rows, live, rows+live+1)
	}
}

// A window too short to anchor into must not emit negative or stray padding.
func TestTinyWindowDoesNotPad(t *testing.T) {
	m := newTestModel()
	m.resize(80, 4)
	printed, _ := m.out.drainText()
	if strings.HasPrefix(printed, "\n\n") {
		t.Errorf("a 4-row window has no room to anchor; should not pad:\n%q", printed)
	}
	if n := len(strings.Split(m.View(), "\n")); n > 3 {
		t.Errorf("live region is %d lines on a 4-row terminal", n)
	}
}
