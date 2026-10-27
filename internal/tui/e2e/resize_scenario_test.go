//go:build darwin || linux

package e2e

import (
	"strings"
	"testing"
	"time"
)

// Resize used to be covered only by hand, because the emulator could not
// resize. These drive drags the way a window manager delivers them — a
// resize every few milliseconds, faster than the 60fps frame tick — against
// a terminal that rewraps (iTerm2, kitty, Ghostty, Terminal.app, …) and one
// that does not (xterm). Before inlineRenderer, the 10–40ms drags stranded
// stacked input boxes, the fastest erased history rows, and on xterm every
// narrowing erased the visible conversation.

// drag resizes from the current width to `to` in steps of two columns.
func drag(tm *Term, from, to, rows int, gap time.Duration) {
	step := 2
	if to < from {
		step = -2
	}
	for w := from + step; (step < 0 && w >= to) || (step > 0 && w <= to); w += step {
		tm.Resize(w, rows)
		time.Sleep(gap)
	}
}

// boxTops counts input-box top borders, scrollback included. A stranded
// frame leaves its border behind, so anything but one is a leak.
func boxTops(s Snapshot) int {
	n := 0
	for _, l := range strings.Split(s.All(), "\n") {
		if strings.Contains(l, "╭") {
			n++
		}
	}
	return n
}

// settled waits for a single, intact live region with the history above it.
func settled(t *testing.T, tm *Term, what string) Snapshot {
	t.Helper()
	time.Sleep(200 * time.Millisecond) // let the last frame land
	return tm.WaitFor(t, what, 5*time.Second, func(s Snapshot) bool {
		return boxTops(s) == 1 && s.InputBox() &&
			strings.Contains(s.StatusLine(), mockModel) &&
			strings.Contains(s.All(), "Hello from the mock model.") &&
			s.HasLine(doneLine)
	})
}

func testDrag(t *testing.T, noReflow bool) {
	env := newEnv(t)
	tm := env.Start(t)
	tm.noReflow = noReflow
	env.Prompt(t, tm, "hello there")
	tm.WaitFor(t, "the turn to finish", 20*time.Second, func(s Snapshot) bool { return s.HasLine(doneLine) })
	for _, gap := range []time.Duration{0, 5 * time.Millisecond, 20 * time.Millisecond} {
		drag(tm, 100, 50, 30, gap)
		settled(t, tm, "one live region after narrowing, gap "+gap.String())
		drag(tm, 50, 100, 30, gap)
		settled(t, tm, "one live region after widening, gap "+gap.String())
	}
}

func TestDragResizeReflowingTerminal(t *testing.T) { testDrag(t, false) }

func TestDragResizeNonReflowingTerminal(t *testing.T) { testDrag(t, true) }

func TestHeightResize(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "hello there")
	tm.WaitFor(t, "the turn to finish", 20*time.Second, func(s Snapshot) bool { return s.HasLine(doneLine) })
	for _, rows := range []int{20, 10, 6, 12, 30} {
		tm.Resize(100, rows)
		time.Sleep(10 * time.Millisecond)
	}
	settled(t, tm, "one live region after the height changed")
}

// A resize while the model is streaming: frames are being redrawn every
// tick and scrollback printed above them as the drag happens.
func TestResizeWhileStreaming(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "slow")
	drag(tm, 100, 60, 30, 5*time.Millisecond)
	drag(tm, 60, 100, 30, 5*time.Millisecond)
	tm.WaitFor(t, "the turn to finish", 30*time.Second, func(s Snapshot) bool { return s.HasLine(doneLine) })
	time.Sleep(200 * time.Millisecond)
	snap := tm.Snapshot()
	if n := boxTops(snap); n != 1 || !snap.InputBox() {
		t.Fatalf("%d input-box borders after resizing mid-stream, want 1\n%s", n, snap)
	}
}

// A program run on the terminal (here $EDITOR, as /open runs it) gets the
// real terminal, not Bubble Tea's muted output, lands below what was already
// printed, and the live region comes back once.
func TestEditorGetsTheTerminal(t *testing.T) {
	env := newEnv(t)
	tm := startTerm(t, klaudiaBin, env.Project, append(env.environ(), "EDITOR=/bin/echo"), []string{"--new-session"}, 100, 30)
	tm.WaitFor(t, "the input box", 20*time.Second, Snapshot.InputBox)
	tm.Type("/open main.go")
	tm.Press(keyEnter)
	snap := tm.WaitFor(t, "the editor's output and the box back", 10*time.Second, func(s Snapshot) bool {
		return strings.Contains(joined(s), "project/main.go") && s.InputBox()
	})
	time.Sleep(200 * time.Millisecond)
	snap = tm.Snapshot()
	all := joined(snap)
	if opening, echoed := strings.Index(all, "Opening main.go"), strings.Index(all, "project/main.go"); opening < 0 || echoed < opening {
		t.Errorf("the editor's output should follow \"Opening main.go…\"\n%s", snap)
	}
	if n := boxTops(snap); n != 1 {
		t.Errorf("%d input-box borders after the editor exited, want 1\n%s", n, snap)
	}
}

// joined is the session with line breaks removed, for text the terminal may
// have wrapped anywhere (a temp-dir path).
func joined(s Snapshot) string { return strings.ReplaceAll(s.All(), "\n", "") }
