//go:build darwin || linux

package e2e

import (
	"slices"
	"testing"
)

// The emulator's own tests run without KLAUDIA_E2E: a harness that misreads
// the terminal would make every scenario's assertion meaningless.

func TestScreenSplitSequencesAndRunes(t *testing.T) {
	s := newScreen(10, 3)
	// "é" and a CSI split across reads, as a pty read boundary can do.
	in := []byte("ab\x1b[2Cé\x1b[1;1Hx")
	for i := range in {
		s.Feed(in[i : i+1])
	}
	if got := s.Snapshot().Lines[0]; got != "xb  é" {
		t.Fatalf("line 0 = %q", got)
	}
}

func TestScreenWideRunesAndScrollback(t *testing.T) {
	s := newScreen(4, 2)
	s.Feed([]byte("世界!\r\nsecond\r\nthird"))
	snap := s.Snapshot()
	// "世界" fills four cells, so "!" wraps; "second" wraps too.
	if want := []string{"世界", "!", "seco", "nd"}; !slices.Equal(snap.Scrollback, want) {
		t.Fatalf("scrollback = %q, want %q", snap.Scrollback, want)
	}
	if want := []string{"thir", "d"}; !slices.Equal(snap.Lines, want) {
		t.Fatalf("lines = %q, want %q", snap.Lines, want)
	}
}

func TestScreenEraseAndCursor(t *testing.T) {
	s := newScreen(10, 3)
	s.Feed([]byte("hello\r\nworld\x1b[1A\x1b[3G\x1b[K\x1b[2;1H\x1b[2K"))
	if got := s.Snapshot().Lines; got[0] != "he" || got[1] != "" {
		t.Fatalf("lines = %q", got)
	}
}

func TestScreenAnswersQueriesInOrder(t *testing.T) {
	s := newScreen(20, 5)
	s.Feed([]byte("ab\x1b]11;?\x1b\\\x1b[6n"))
	want := "\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[1;3R"
	if got := string(s.takeReplies()); got != want {
		t.Fatalf("replies = %q, want %q (OSC 11 first, then the cursor report)", got, want)
	}
	if got := s.takeReplies(); len(got) != 0 {
		t.Fatalf("replies not cleared: %q", got)
	}
	if snap := s.Snapshot(); snap.Lines[0] != "ab" {
		t.Fatalf("a query leaked onto the screen: %q", snap.Lines[0])
	}
}

func TestScreenRecordsOSC8Links(t *testing.T) {
	s := newScreen(40, 2)
	s.Feed([]byte("see \x1b]8;id=k1;file:///p/main.go\x1b\\main.go\x1b]8;;\x1b\\ and \x1b]8;;https://go.dev\x07go.dev\x1b]8;;\x07"))
	snap := s.Snapshot()
	if want := []string{"file:///p/main.go", "https://go.dev"}; !slices.Equal(snap.Links, want) {
		t.Fatalf("links = %q, want %q", snap.Links, want)
	}
	if snap.Lines[0] != "see main.go and go.dev" {
		t.Fatalf("line 0 = %q", snap.Lines[0])
	}
}

func TestScreenReflowJoinsSoftWrapsOnly(t *testing.T) {
	s := newScreen(6, 4)
	// "abcdefgh" soft-wraps; "xy" is its own line; the cursor ends after "z".
	s.Feed([]byte("abcdefgh\r\nxy\r\nz"))
	s.Resize(10, 4, true)
	snap := s.Snapshot()
	if want := []string{"abcdefgh", "xy", "z", ""}; !slices.Equal(snap.Lines, want) {
		t.Fatalf("widened: lines = %q, want %q", snap.Lines, want)
	}
	if snap.CursorX != 1 || snap.CursorY != 2 {
		t.Fatalf("widened: cursor = (%d,%d), want (1,2)", snap.CursorX, snap.CursorY)
	}
	s.Resize(3, 4, true)
	snap = s.Snapshot()
	// abc|def|gh|xy|z: five rows on a four-row screen, so one goes to scrollback.
	if want := []string{"def", "gh", "xy", "z"}; !slices.Equal(snap.Lines, want) {
		t.Fatalf("narrowed: lines = %q, want %q", snap.Lines, want)
	}
	if want := []string{"abc"}; !slices.Equal(snap.Scrollback, want) {
		t.Fatalf("narrowed: scrollback = %q, want %q", snap.Scrollback, want)
	}
	if snap.CursorX != 1 || snap.CursorY != 3 {
		t.Fatalf("narrowed: cursor = (%d,%d), want (1,3)", snap.CursorX, snap.CursorY)
	}
	// Growing again pulls the row back out of scrollback.
	s.Resize(3, 5, true)
	if snap = s.Snapshot(); snap.Lines[0] != "abc" || len(snap.Scrollback) != 0 {
		t.Fatalf("grown: %s", snap)
	}
}

func TestScreenReflowCursorAtLineStart(t *testing.T) {
	// Bubble Tea leaves the cursor at column 0 of the frame's last line. When
	// that line rewraps, the cursor stays on its first row.
	s := newScreen(8, 4)
	s.Feed([]byte("top\r\nabcdefg\r"))
	s.Resize(4, 4, true)
	snap := s.Snapshot()
	if snap.CursorY != 1 || snap.CursorX != 0 || snap.Lines[1] != "abcd" || snap.Lines[2] != "efg" {
		t.Fatalf("cursor (%d,%d)\n%s", snap.CursorX, snap.CursorY, snap)
	}
}

func TestScreenClipDoesNotRewrap(t *testing.T) {
	s := newScreen(6, 3)
	s.Feed([]byte("abcdefgh\r\nxy"))
	s.Resize(4, 3, false)
	snap := s.Snapshot()
	if want := []string{"abcd", "gh", "xy"}; !slices.Equal(snap.Lines, want) {
		t.Fatalf("lines = %q, want %q", snap.Lines, want)
	}
}

func TestScreenEraseEndsSoftWrap(t *testing.T) {
	s := newScreen(4, 3)
	s.Feed([]byte("abcdef\x1b[1;3H\x1b[K"))
	s.Resize(8, 3, true)
	if want := []string{"ab", "ef"}; !slices.Equal(s.Snapshot().Lines[:2], want) {
		t.Fatalf("lines = %q, want %q", s.Snapshot().Lines, want)
	}
}

func TestScreenAutowrapOffClips(t *testing.T) {
	s := newScreen(4, 3)
	s.Feed([]byte("\x1b[?7labcdef\x1b[?7h\r\nxyzwv"))
	if want := []string{"abcf", "xyzw", "v"}; !slices.Equal(s.Snapshot().Lines, want) {
		t.Fatalf("lines = %q, want %q", s.Snapshot().Lines, want)
	}
}

func TestScreenRecordsBellsAndNotifications(t *testing.T) {
	s := newScreen(20, 3)
	s.Feed([]byte("a\a\x1b]9;hello\ab\x1b]8;;http://x\a"))
	snap := s.Snapshot()
	if snap.Bells != 1 || len(snap.Notes) != 1 || snap.Notes[0] != "hello" || snap.Lines[0] != "ab" {
		t.Fatalf("bells %d notes %q lines %q", snap.Bells, snap.Notes, snap.Lines)
	}
}
