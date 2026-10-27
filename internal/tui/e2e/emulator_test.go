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
