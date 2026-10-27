package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// These read the bytes inlineRenderer writes. What reaches a real terminal —
// resizes, drags, a terminal that does not rewrap — is covered end to end by
// the e2e emulator (internal/tui/e2e/resize_scenario_test.go); these pin the
// invariants that make those pass.

func newTestRenderer(w, h int) (*inlineRenderer, *bytes.Buffer) {
	var out bytes.Buffer
	r := newInlineRenderer(&out)
	r.resize(w, h)
	return r, &out
}

// flushed draws and returns what was written.
func flushed(r *inlineRenderer, out *bytes.Buffer) string {
	out.Reset()
	r.flush()
	return out.String()
}

// The invariant: every flush leaves the cursor at column 0 of the frame's
// first line, so a resize finds the top of the old frame where the cursor is.
func TestRendererReturnsToTheFrameTop(t *testing.T) {
	r, out := newTestRenderer(80, 24)
	r.setFrame("one\ntwo\nthree")
	got := flushed(r, out)
	if !strings.HasSuffix(got, "\r"+ansi.CursorUp(2)+wrapOn+syncEnd) {
		t.Fatalf("a three-line frame should end by moving up two rows: %q", got)
	}
	r.setFrame("one\nTWO\nthree")
	if got := flushed(r, out); !strings.HasSuffix(got, "\r"+ansi.CursorUp(2)+wrapOn+syncEnd) {
		t.Fatalf("an updated frame should end at the top too: %q", got)
	}
}

// A line that reaches the last column leaves the cursor in the pending-wrap
// state, and one wider than the terminal wraps into a row the move back up
// does not count.
func TestRendererNeverWritesToTheLastColumn(t *testing.T) {
	for _, width := range []int{120, 80, 40, 10} {
		r, out := newTestRenderer(width, 24)
		r.setFrame(strings.Repeat("x", width+30) + "\n" + strings.Repeat("y", width))
		got := ansi.Strip(flushed(r, out))
		for _, row := range strings.Split(got, "\n") {
			if w := ansi.StringWidth(strings.Trim(row, "\r")); w >= width {
				t.Errorf("width %d: a row occupies %d cells", width, w)
			}
		}
	}
}

// The frame never exceeds the screen: the move back up cannot go past the
// top row.
func TestRendererKeepsTheFrameShorterThanTheScreen(t *testing.T) {
	r, out := newTestRenderer(80, 4)
	r.setFrame("1\n2\n3\n4\n5\n6")
	got := flushed(r, out)
	if strings.Contains(got, "1\x1b") || strings.Contains(got, "2\x1b") || strings.Contains(got, "3\x1b") {
		t.Fatalf("lines above the screen were drawn: %q", got)
	}
	if !strings.HasSuffix(got, ansi.CursorUp(2)+wrapOn+syncEnd) {
		t.Fatalf("want the last three lines and a move up of two: %q", got)
	}
}

// After a resize the old frame is everything from the cursor down, however
// the terminal rewrapped it — so it is erased, not diffed.
func TestRendererRedrawsFromTheTopAfterAResize(t *testing.T) {
	r, out := newTestRenderer(80, 24)
	r.setFrame("box\nstatus")
	flushed(r, out)
	r.resize(50, 24)
	got := flushed(r, out)
	if !strings.HasPrefix(got, syncBegin+"\r"+ansi.EraseScreenBelow) {
		t.Fatalf("a resize should erase from the frame top: %q", got)
	}
	if !strings.Contains(got, "box") || !strings.Contains(got, "status") {
		t.Fatalf("every line should be redrawn: %q", got)
	}
}

// Scrollback goes above the frame, with autowrap on so long lines wrap and
// the terminal can rewrap them later; the frame is drawn with it off.
func TestRendererPrintsAboveTheFrame(t *testing.T) {
	r, out := newTestRenderer(80, 24)
	r.setFrame("box")
	flushed(r, out)
	r.print("history one\nhistory two")
	got := flushed(r, out)
	h, off, box := strings.Index(got, "history two"), strings.Index(got, wrapOff), strings.Index(got, "box")
	if h < 0 || off < h || box < off {
		t.Fatalf("want history, then autowrap off, then the frame: %q", got)
	}
}

// Without a resize or anything printed, only changed lines are written.
func TestRendererRewritesOnlyChangedLines(t *testing.T) {
	r, out := newTestRenderer(80, 24)
	r.setFrame("same\nold\nsame too")
	flushed(r, out)
	r.setFrame("same\nnew\nsame too")
	got := flushed(r, out)
	if strings.Contains(got, "same") || !strings.Contains(got, "new") {
		t.Fatalf("only the changed line should be written: %q", got)
	}
	if strings.Contains(got, ansi.EraseScreenBelow) {
		t.Fatalf("nothing shrank, so nothing needs erasing: %q", got)
	}
}

// A frame that shrinks erases the rows it no longer uses.
func TestRendererErasesWhatAShorterFrameLeaves(t *testing.T) {
	r, out := newTestRenderer(80, 24)
	r.setFrame("a\nb\nc\nd")
	flushed(r, out)
	r.setFrame("a\nb")
	got := flushed(r, out)
	if !strings.Contains(got, ansi.EraseScreenBelow) {
		t.Fatalf("the two rows left over were not erased: %q", got)
	}
	// Two lines, then one row down to erase: back up two.
	if !strings.HasSuffix(got, "\r"+ansi.CursorUp(2)+wrapOn+syncEnd) {
		t.Fatalf("want a move back up of two: %q", got)
	}
}

// Quitting draws an empty frame, which erases the live region.
func TestRendererEmptyFrameErases(t *testing.T) {
	r, out := newTestRenderer(80, 24)
	r.setFrame("box\nstatus")
	flushed(r, out)
	r.setFrame("")
	got := flushed(r, out)
	if !strings.Contains(got, ansi.EraseScreenBelow) || strings.Contains(got, ansi.CursorUp(1)) {
		t.Fatalf("an empty frame should erase and stay put: %q", got)
	}
}

// A pager gets a clean terminal and the frame comes back after.
func TestRendererSuspendAndResume(t *testing.T) {
	r, out := newTestRenderer(80, 24)
	r.setFrame("box")
	flushed(r, out)
	out.Reset()
	r.suspend()
	if got := out.String(); !strings.Contains(got, ansi.EraseScreenBelow) || !strings.Contains(got, ansi.ShowCursor) {
		t.Fatalf("suspend should erase the frame and show the cursor: %q", got)
	}
	r.setFrame("box2")
	if got := flushed(r, out); got != "" {
		t.Fatalf("nothing should be drawn while suspended: %q", got)
	}
	r.resume()
	if got := flushed(r, out); !strings.Contains(got, "box2") {
		t.Fatalf("resume should redraw: %q", got)
	}
}
