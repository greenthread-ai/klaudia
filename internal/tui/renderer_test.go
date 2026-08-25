package tui

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// These tests drive the real Bubble Tea renderer against a buffer and read the
// bytes it actually emits.
//
// Every rendering bug in this package so far has lived in the gap between what
// View returns and what the renderer does with it — full-width lines that never
// got an EraseLineRight, cursor arithmetic that assumed no reflow. Asserting on
// View alone cannot see any of that, and two such bugs shipped past tests that
// did exactly that. This is the only place the renderer itself is under test.

var (
	csiAny      = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")
	csiCursorUp = regexp.MustCompile("\x1b\\[([0-9]+)A")
)

// renderBytes runs the program through a scripted sequence and returns
// everything written to the terminal.
func renderBytes(t *testing.T, script func(p *tea.Program)) string {
	t.Helper()
	var out bytes.Buffer
	p := tea.NewProgram(newTestModel(),
		tea.WithInput(strings.NewReader("")),
		tea.WithOutput(&out),
		tea.WithoutSignalHandler(),
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		script(p)
		p.Quit()
	}()
	if _, err := p.Run(); err != nil {
		t.Fatalf("program: %v", err)
	}
	<-done
	return out.String()
}

// rowWidths measures how many cells each emitted row actually occupies.
//
// A bare \r is a carriage return, not a newline: it restarts the same row at
// column 0, so what follows overwrites what came before rather than adding to
// it. The first version of this helper concatenated across them and reported a
// 119-cell border as 120 — a test that would have failed a correct renderer.
// The width of a row is the widest of its carriage-return-separated runs.
func rowWidths(raw string) []int {
	var out []int
	for _, chunk := range strings.Split(raw, "\r\n") {
		for _, row := range strings.Split(chunk, "\n") {
			widest := 0
			for _, run := range strings.Split(row, "\r") {
				if w := ansi.StringWidth(csiAny.ReplaceAllString(run, "")); w > widest {
					widest = w
				}
			}
			out = append(out, widest)
		}
	}
	return out
}

// The invariant two separate bugs came down to. A line that fills the last
// column gets no EraseLineRight from the renderer and counts as two physical
// rows, which is what strands the live region in scrollback.
func TestRendererNeverWritesToTheLastColumn(t *testing.T) {
	for _, width := range []int{120, 100, 80, 60, 40} {
		raw := renderBytes(t, func(p *tea.Program) {
			time.Sleep(50 * time.Millisecond)
			p.Send(tea.WindowSizeMsg{Width: width, Height: 30})
			time.Sleep(50 * time.Millisecond)
			p.Send(tea.Println(strings.Repeat("scrollback ", 30)))
			time.Sleep(80 * time.Millisecond)
		})
		for i, w := range rowWidths(raw) {
			if w >= width {
				t.Errorf("terminal width %d: emitted row %d occupies %d cells", width, i, w)
			}
		}
	}
}

// Shrinking reflows the rows already on screen, so the renderer's own
// CursorUp(linesRendered-1) lands inside the old frame. The correction has to
// reach the wire, not just the unit test for the arithmetic.
func TestShrinkingEmitsTheReflowCorrection(t *testing.T) {
	raw := renderBytes(t, func(p *tea.Program) {
		time.Sleep(50 * time.Millisecond)
		p.Send(tea.WindowSizeMsg{Width: 100, Height: 30})
		time.Sleep(50 * time.Millisecond)
		p.Send(tea.WindowSizeMsg{Width: 30, Height: 30})
		time.Sleep(80 * time.Millisecond)
	})
	// The live region is a handful of lines, so the renderer's own move is
	// small. A correction for a 100→30 reflow is necessarily much larger.
	biggest := 0
	for _, m := range csiCursorUp.FindAllStringSubmatch(raw, -1) {
		if n, _ := strconv.Atoi(m[1]); n > biggest {
			biggest = n
		}
	}
	if biggest < 6 {
		t.Errorf("largest CursorUp was %d; a 100→30 reflow needs far more, so the "+
			"correction never reached the terminal", biggest)
	}
	if !strings.Contains(raw, ansi.EraseScreenBelow) {
		t.Error("no EraseScreenBelow: the orphaned rows would survive")
	}
}

// Growing rewraps nothing, so a correction would scroll the conversation for no
// reason.
func TestGrowingEmitsNoReflowCorrection(t *testing.T) {
	raw := renderBytes(t, func(p *tea.Program) {
		time.Sleep(50 * time.Millisecond)
		p.Send(tea.WindowSizeMsg{Width: 60, Height: 30})
		time.Sleep(50 * time.Millisecond)
		p.Send(tea.WindowSizeMsg{Width: 140, Height: 30})
		time.Sleep(80 * time.Millisecond)
	})
	for _, m := range csiCursorUp.FindAllStringSubmatch(raw, -1) {
		if n, _ := strconv.Atoi(m[1]); n > 6 {
			t.Errorf("growing emitted CursorUp(%d); nothing rewrapped, so nothing "+
				"needed correcting", n)
		}
	}
}

// Scrollback printed while the live region is up must not leave residue: this
// is the "› Ask Klaudia…" stranded mid-history.
func TestScrollbackDoesNotStrandTheLiveRegion(t *testing.T) {
	const width = 100
	raw := renderBytes(t, func(p *tea.Program) {
		time.Sleep(50 * time.Millisecond)
		p.Send(tea.WindowSizeMsg{Width: width, Height: 30})
		for i := 0; i < 3; i++ {
			time.Sleep(40 * time.Millisecond)
			p.Send(tea.Println(strings.Repeat("tool output ", 20)))
		}
		time.Sleep(80 * time.Millisecond)
	})
	widths := rowWidths(raw)
	// Every row the renderer writes must be erasable, which is the same as
	// being narrower than the terminal.
	for i, w := range widths {
		if w >= width {
			t.Fatalf("row %d occupies %d cells, so it carries no EraseLineRight", i, w)
		}
	}
	if n := strings.Count(raw, ansi.EraseLineRight); n < len(widths)/2 {
		t.Errorf("only %d EraseLineRight for %d rows — most rows are not cleaning up",
			n, len(widths))
	}
}
