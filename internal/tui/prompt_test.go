package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestInputIsFramed(t *testing.T) {
	m := newTestModel()
	m.resize(72, 24)
	view := visibleText(m.View())

	if !strings.Contains(view, "╭") || !strings.Contains(view, "╰") {
		t.Fatalf("expected the input to be boxed:\n%s", view)
	}
	// The box spans the terminal bar one column, so it still reads as a widget
	// rather than a fragment — but never occupies the last cell. See
	// resizeResync: Bubble Tea only erases the rest of a row for lines
	// *narrower* than the terminal, and writing the final column parks the
	// cursor in the pending-wrap state. Both matter on resize.
	for _, ln := range strings.Split(view, "\n") {
		if strings.HasPrefix(ln, "╭") || strings.HasPrefix(ln, "╰") {
			if w := len([]rune(ln)); w != 71 {
				t.Errorf("border row is %d cells wide, want 71 (width-1): %q", w, ln)
			}
		}
	}
}

// The invariant the resize bug came down to: nothing in the live region may
// occupy the terminal's last column, at any width.
//
// A full-width line gets no EraseLineRight from the renderer, so stale content
// to its right survives; and it leaves the cursor in the terminal's
// pending-wrap state. Three of the four live-region lines used to be exactly
// the terminal width.
func TestLiveRegionNeverFillsTheLastColumn(t *testing.T) {
	for _, w := range []int{160, 144, 120, 100, 80, 72, 60, 40, 30} {
		m := newTestModel()
		m.resize(w, 40)
		for i, ln := range strings.Split(visibleText(m.View()), "\n") {
			if got := len([]rune(ln)); got >= w {
				t.Errorf("at width %d, live-region line %d is %d cells: %q", w, i, got, ln)
			}
		}
	}
}

// The status line is a caption on the box, not a full-width bar. That framing
// is the whole point: it belongs to something visible, so it no longer reads as
// chrome that has drifted up the screen.
func TestStatusLineIsCaptionedUnderTheBox(t *testing.T) {
	m := newTestModel()
	m.resize(72, 24)
	lines := strings.Split(visibleText(m.View()), "\n")

	last := lines[len(lines)-1]
	if !strings.Contains(last, "turns") {
		t.Fatalf("last line should be the status caption, got %q", last)
	}
	if !strings.HasPrefix(last, "  ") {
		t.Errorf("status should be indented under the box, got %q", last)
	}
	// It sits below the bottom border, not inside the box.
	var borderIdx int
	for i, ln := range lines {
		if strings.HasPrefix(ln, "╰") {
			borderIdx = i
		}
	}
	if borderIdx == 0 || borderIdx >= len(lines)-1 {
		t.Errorf("status should follow the closing border:\n%s", strings.Join(lines, "\n"))
	}
}

// Two rows and two columns are a real cost on a small terminal; below the
// threshold the input is drawn bare rather than eating the screen.
func TestFramingDegradesOnSmallTerminals(t *testing.T) {
	for _, tc := range []struct {
		w, h  int
		boxed bool
	}{
		{72, 24, true},
		{30, 10, true},
		{72, 9, false},  // too short
		{29, 24, false}, // too narrow
	} {
		m := newTestModel()
		m.resize(tc.w, tc.h)
		got := strings.Contains(visibleText(m.View()), "╭")
		if got != tc.boxed {
			t.Errorf("%dx%d: boxed = %v, want %v", tc.w, tc.h, got, tc.boxed)
		}
	}
}

// The marker matches the one the transcript uses for submitted prompts, and
// appears once — repeating it down a wrapped paste would read as many prompts.
func TestPromptMarkerAppearsOnceOnWrappedInput(t *testing.T) {
	m := newTestModel()
	m.resize(60, 24)
	m.input.SetValue(strings.Repeat("wrapped text ", 12))

	view := visibleText(m.View())
	if n := strings.Count(view, "›"); n != 1 {
		t.Errorf("prompt marker appears %d times, want 1:\n%s", n, view)
	}
	if strings.Count(view, "│") < 6 {
		t.Errorf("expected a multi-row box for wrapped input:\n%s", view)
	}
}

// Framing must not push the live region past the terminal; that is exactly the
// regime that desynchronised the inline renderer's cursor arithmetic.
func TestFramedRegionStaysWithinTerminal(t *testing.T) {
	for _, h := range []int{10, 12, 24, 60} {
		m := newTestModel()
		m.resize(80, h)
		m.input.SetValue(strings.Repeat("long wrapped input ", 30))
		if n := len(strings.Split(m.View(), "\n")); n >= h {
			t.Errorf("height %d: live region is %d lines, must leave room", h, n)
		}
	}
}

func TestPlaceholderFitsANarrowBox(t *testing.T) {
	m := newTestModel()
	m.resize(minBoxWidth, 24)
	for _, ln := range strings.Split(visibleText(m.View()), "\n") {
		if w := len([]rune(ln)); w > minBoxWidth {
			t.Errorf("line overflows a %d-column terminal (%d cells): %q", minBoxWidth, w, ln)
		}
	}
}

func TestStatusCaptionDropsSegmentsNotCharacters(t *testing.T) {
	m := newTestModel()
	m.sess.ResolvedModel = "claude-opus-5"
	m.sess.PermissionMode = "plan"
	m.statTurns, m.statIn, m.statOut = 3, 1000, 200

	wide := visibleText(m.statusLineAt(200))
	for _, want := range []string{"claude-opus-5", "plan", "3 turns", "1.2k tokens"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide caption missing %q: %q", want, wide)
		}
	}

	narrow := visibleText(m.statusLineAt(24))
	if len([]rune(narrow)) > 24 {
		t.Errorf("narrow caption is %d cells, want ≤24: %q", len([]rune(narrow)), narrow)
	}
	if !strings.Contains(narrow, "claude-opus-5") {
		t.Errorf("the model must survive truncation: %q", narrow)
	}
	// Dropped whole segments, so no dangling fragment of one.
	if strings.HasSuffix(strings.TrimSpace(narrow), "·") {
		t.Errorf("caption ends mid-separator: %q", narrow)
	}
	for _, ln := range strings.Split(narrow, " · ") {
		if strings.HasPrefix(ln, "0 tur") && ln != "0 turns" {
			t.Errorf("a segment was cut mid-word: %q", narrow)
		}
	}
}

// An unattended goal loop outranks the counters: it says the session is doing
// something on its own, which is what you would most want to notice.
func TestStatusCaptionKeepsGoalStateWhenNarrow(t *testing.T) {
	m := newTestModel()
	m.sess.ResolvedModel = "opus"
	m.loopRemaining, m.loopTotal = 2, 5

	narrow := visibleText(m.statusLineAt(26))
	if !strings.Contains(narrow, "goal 4/5") {
		t.Errorf("goal state should outrank turn/token counters: %q", narrow)
	}
}

// Before the first WindowSizeMsg the width is zero — unknown, not tiny.
func TestStatusCaptionKeepsEverythingAtUnknownWidth(t *testing.T) {
	m := newTestModel()
	m.sess.ResolvedModel = "claude-opus-5"
	if got := visibleText(m.statusLine()); !strings.Contains(got, "tokens") {
		t.Errorf("unknown width should not truncate: %q", got)
	}
}

// The arithmetic the resize fix rests on: how many extra physical rows the
// previous frame occupies once the terminal reflows it.
func TestReflowDeficit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		widths   []int
		newWidth int
		want     int
	}{
		{"no previous frame", nil, 90, 0},
		{"unknown width", []int{119, 119}, 0, 0},
		// Growing: every line already fitted, so nothing rewraps.
		{"window grew", []int{89, 89, 89, 40}, 120, 0},
		{"width unchanged", []int{89, 89, 89, 40}, 90, 0},
		// 120 → 90: three 119-wide lines each become two rows.
		{"shrank a little", []int{119, 119, 119, 60}, 90, 3},
		// 120 → 40: 119 needs three rows each, 60 needs two.
		{"shrank a lot", []int{119, 119, 119, 60}, 40, 7},
		// Exactly the width is one row, not two.
		{"exact fit", []int{90, 90}, 90, 0},
		{"one over", []int{91, 91}, 90, 2},
	} {
		if got := reflowDeficit(tc.widths, tc.newWidth); got != tc.want {
			t.Errorf("%s: reflowDeficit(%v, %d) = %d, want %d",
				tc.name, tc.widths, tc.newWidth, got, tc.want)
		}
	}
}

// A resize must prefix the next frame with the cursor motion that undoes the
// reflow, or the renderer paints below the old frame and orphans it.
func TestResizeSteersTheCursorBack(t *testing.T) {
	m := newTestModel()
	m.resize(120, 40)
	_ = m.View() // records the frame's geometry

	m.update(tea.WindowSizeMsg{Width: 90, Height: 40})
	out := m.View()

	if !strings.Contains(out, ansi.EraseScreenBelow) {
		t.Error("the resize frame does not erase the orphaned rows")
	}
	if !strings.Contains(out, ansi.CursorUp(3)) {
		t.Errorf("expected CursorUp(3) for a 120→90 reflow of a 4-line region, got %q",
			visibleEscapes(out))
	}
	// And only once: a second frame at the same size must not keep moving up.
	if again := m.View(); strings.Contains(again, ansi.EraseScreenBelow) {
		t.Error("the reflow prefix was emitted twice")
	}
}

// Growing the window rewraps nothing, so no correction is needed — emitting one
// would scroll the conversation for no reason.
func TestGrowingNeedsNoCorrection(t *testing.T) {
	m := newTestModel()
	m.resize(90, 40)
	_ = m.View()
	m.update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if out := m.View(); strings.Contains(out, ansi.EraseScreenBelow) {
		t.Errorf("growing emitted a reflow correction: %q", visibleEscapes(out))
	}
}

// visibleEscapes renders the CSI sequences in a string readably.
func visibleEscapes(s string) string {
	return strings.ReplaceAll(s, "\x1b", "\\e")
}
