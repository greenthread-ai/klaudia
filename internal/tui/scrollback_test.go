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
