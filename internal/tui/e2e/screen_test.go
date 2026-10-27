//go:build darwin || linux

package e2e

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Screen is a minimal VT emulator: enough of CSI cursor movement and erase,
// scrolling into scrollback, and OSC handling to reconstruct what Bubble
// Tea's inline renderer leaves on a terminal. Colours and attributes are
// dropped. Wide runes take two cells.
//
// It also answers terminal queries — OSC 10/11 colour queries and CSI 6n
// cursor reports — in the order they appear in the output. Order matters:
// termenv sends OSC 11 then 6n and reads until the 6n reply, so answering 6n
// first makes it give up and the late OSC 11 reply lands in the prompt.
type Screen struct {
	cols, rows int
	grid       [][]string // one entry per cell; "" is the right half of a wide rune
	scrollback []string
	x, y       int
	savedX     int
	savedY     int
	pending    []byte // an escape sequence or UTF-8 rune split across reads
	replies    []byte // answers to queries, in order, not yet written
	links      []string
}

func newScreen(cols, rows int) *Screen {
	s := &Screen{cols: cols, rows: rows}
	s.grid = make([][]string, rows)
	for i := range s.grid {
		s.grid[i] = s.blankRow()
	}
	return s
}

func (s *Screen) blankRow() []string {
	r := make([]string, s.cols)
	for i := range r {
		r[i] = " "
	}
	return r
}

func (s *Screen) rowText(r []string) string {
	return strings.TrimRight(strings.Join(r, ""), " ")
}

// lf moves down a line, scrolling the top row into scrollback at the bottom.
func (s *Screen) lf() {
	if s.y == s.rows-1 {
		s.scrollUp(1)
		return
	}
	s.y++
}

func (s *Screen) scrollUp(n int) {
	for range min(n, s.rows) {
		s.scrollback = append(s.scrollback, s.rowText(s.grid[0]))
		copy(s.grid, s.grid[1:])
		s.grid[s.rows-1] = s.blankRow()
	}
}

func (s *Screen) put(r rune) {
	w := runewidth.RuneWidth(r)
	if w == 0 {
		// Combining mark or zero-width joiner: attach to the previous cell.
		if s.x > 0 && s.x <= s.cols {
			s.grid[s.y][s.x-1] += string(r)
		}
		return
	}
	// Deferred wrap: the cursor may sit one past the last column after
	// writing there, and only the next printable rune wraps.
	if s.x+w > s.cols {
		s.x = 0
		s.lf()
	}
	s.grid[s.y][s.x] = string(r)
	if w == 2 && s.x+1 < s.cols {
		s.grid[s.y][s.x+1] = ""
	}
	s.x += w
}

func (s *Screen) clampX(x int) int { return max(0, min(s.cols-1, x)) }
func (s *Screen) clampY(y int) int { return max(0, min(s.rows-1, y)) }

func (s *Screen) eraseCells(y, from, to int) {
	for i := max(0, from); i < min(s.cols, to); i++ {
		s.grid[y][i] = " "
	}
}

func (s *Screen) csi(params string, final byte) {
	if params != "" && strings.ContainsAny(params[:1], "?><=") {
		return // private modes (cursor visibility, bracketed paste, …)
	}
	var nums []int
	if params != "" {
		for _, p := range strings.Split(params, ";") {
			n, _ := strconv.Atoi(p)
			nums = append(nums, n)
		}
	}
	arg := func(i, def int) int {
		if i < len(nums) && nums[i] != 0 {
			return nums[i]
		}
		return def
	}
	n := arg(0, 1)
	switch final {
	case 'A':
		s.y = s.clampY(s.y - n)
	case 'B':
		s.y = s.clampY(s.y + n)
	case 'C':
		s.x = s.clampX(s.x + n)
	case 'D':
		s.x = s.clampX(min(s.x, s.cols-1) - n)
	case 'E':
		s.x, s.y = 0, s.clampY(s.y+n)
	case 'F':
		s.x, s.y = 0, s.clampY(s.y-n)
	case 'G':
		s.x = s.clampX(n - 1)
	case 'd':
		s.y = s.clampY(n - 1)
	case 'H', 'f':
		s.y, s.x = s.clampY(arg(0, 1)-1), s.clampX(arg(1, 1)-1)
	case 'K':
		switch arg(0, 0) {
		case 0:
			s.eraseCells(s.y, s.x, s.cols)
		case 1:
			s.eraseCells(s.y, 0, s.x+1)
		case 2:
			s.eraseCells(s.y, 0, s.cols)
		}
	case 'J':
		switch arg(0, 0) {
		case 0:
			s.eraseCells(s.y, s.x, s.cols)
			for y := s.y + 1; y < s.rows; y++ {
				s.grid[y] = s.blankRow()
			}
		case 1:
			for y := 0; y < s.y; y++ {
				s.grid[y] = s.blankRow()
			}
			s.eraseCells(s.y, 0, s.x+1)
		case 2, 3:
			for y := range s.grid {
				s.grid[y] = s.blankRow()
			}
		}
	case 'S':
		s.scrollUp(n)
	case 'T':
		for range min(n, s.rows) {
			copy(s.grid[1:], s.grid[:s.rows-1])
			s.grid[0] = s.blankRow()
		}
	case 'L': // insert lines at the cursor row
		for range min(n, s.rows-s.y) {
			copy(s.grid[s.y+1:], s.grid[s.y:s.rows-1])
			s.grid[s.y] = s.blankRow()
		}
	case 'M': // delete lines at the cursor row
		for range min(n, s.rows-s.y) {
			copy(s.grid[s.y:], s.grid[s.y+1:])
			s.grid[s.rows-1] = s.blankRow()
		}
	case 'n':
		if arg(0, 0) == 6 {
			s.replies = fmt.Appendf(s.replies, "\x1b[%d;%dR", s.y+1, min(s.x, s.cols-1)+1)
		}
	}
}

func (s *Screen) osc(body string) {
	switch {
	case body == "10;?":
		s.replies = append(s.replies, "\x1b]10;rgb:ffff/ffff/ffff\x1b\\"...)
	case body == "11;?":
		s.replies = append(s.replies, "\x1b]11;rgb:0000/0000/0000\x1b\\"...)
	case strings.HasPrefix(body, "8;"):
		// OSC 8 ; params ; URI — an empty URI closes the link.
		parts := strings.SplitN(body, ";", 3)
		if len(parts) == 3 && parts[2] != "" {
			s.links = append(s.links, parts[2])
		}
	}
}

// Feed interprets terminal output. Sequences split across calls are carried
// over to the next one.
func (s *Screen) Feed(data []byte) {
	if len(s.pending) > 0 {
		data = append(s.pending, data...)
		s.pending = nil
	}
	i := 0
	for i < len(data) {
		c := data[i]
		if c == 0x1b {
			n, ok := s.escape(data[i:])
			if !ok {
				s.pending = append([]byte(nil), data[i:]...)
				return
			}
			i += n
			continue
		}
		if c < 0x20 || c == 0x7f {
			switch c {
			case '\r':
				s.x = 0
			case '\n':
				s.lf()
			case '\b':
				s.x = max(0, min(s.x, s.cols-1)-1)
			case '\t':
				s.x = s.clampX((s.x/8 + 1) * 8)
			}
			i++
			continue
		}
		if !utf8.FullRune(data[i:]) {
			s.pending = append([]byte(nil), data[i:]...)
			return
		}
		r, size := utf8.DecodeRune(data[i:])
		s.put(r)
		i += size
	}
}

// escape consumes one escape sequence at the start of b, returning its
// length, or ok=false when b ends before the sequence does.
func (s *Screen) escape(b []byte) (n int, ok bool) {
	if len(b) < 2 {
		return 0, false
	}
	switch b[1] {
	case '[':
		j := 2
		for j < len(b) && b[j] >= 0x30 && b[j] <= 0x3f {
			j++
		}
		params := string(b[2:j])
		for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2f {
			j++
		}
		if j >= len(b) {
			return 0, false
		}
		s.csi(params, b[j])
		return j + 1, true
	case ']', 'P', '_', '^': // OSC, DCS, APC, PM: run to BEL or ST
		for j := 2; j < len(b); j++ {
			if b[j] == 0x07 {
				if b[1] == ']' {
					s.osc(string(b[2:j]))
				}
				return j + 1, true
			}
			if b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\' {
				if b[1] == ']' {
					s.osc(string(b[2:j]))
				}
				return j + 2, true
			}
		}
		return 0, false
	case '(', ')', '*', '+':
		if len(b) < 3 {
			return 0, false
		}
		return 3, true
	case '7':
		s.savedX, s.savedY = s.x, s.y
	case '8':
		s.x, s.y = s.savedX, s.savedY
	case 'M': // reverse index
		if s.y == 0 {
			copy(s.grid[1:], s.grid[:s.rows-1])
			s.grid[0] = s.blankRow()
		} else {
			s.y--
		}
	case 'D':
		s.lf()
	case 'E':
		s.x = 0
		s.lf()
	}
	return 2, true
}

// takeReplies returns the queued query answers and clears the queue.
func (s *Screen) takeReplies() []byte {
	r := s.replies
	s.replies = nil
	return r
}

// Snapshot copies the current state.
func (s *Screen) Snapshot() Snapshot {
	snap := Snapshot{
		Cols: s.cols, Rows: s.rows, CursorX: s.x, CursorY: s.y,
		Scrollback: append([]string(nil), s.scrollback...),
		Links:      append([]string(nil), s.links...),
	}
	for _, r := range s.grid {
		snap.Lines = append(snap.Lines, s.rowText(r))
	}
	return snap
}

// Snapshot is what the terminal showed at one moment.
type Snapshot struct {
	Cols, Rows       int
	CursorX, CursorY int
	// Lines is the visible screen, one entry per row, trailing spaces trimmed.
	Lines []string
	// Scrollback is every line that scrolled off the top, oldest first.
	Scrollback []string
	// Links holds the URI of every OSC 8 hyperlink opened so far.
	Links []string
}

// Screen is the visible screen as text.
func (s Snapshot) Screen() string { return strings.Join(s.Lines, "\n") }

// All is scrollback followed by the visible screen: the whole session as a
// user scrolling up would read it.
func (s Snapshot) All() string {
	return strings.Join(append(append([]string(nil), s.Scrollback...), s.Lines...), "\n")
}

// HasLine reports whether any line, scrollback included, matches re.
func (s Snapshot) HasLine(re *regexp.Regexp) bool { return len(s.MatchLines(re)) > 0 }

// MatchLines returns the lines, scrollback included, that match re.
func (s Snapshot) MatchLines(re *regexp.Regexp) []string {
	var out []string
	for _, l := range append(append([]string(nil), s.Scrollback...), s.Lines...) {
		if re.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

// StatusLine is the last non-blank row below the input box's bottom border.
func (s Snapshot) StatusLine() string {
	for i := len(s.Lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(s.Lines[i]), "╰") {
			// The last non-blank line below the box: panels (running jobs)
			// can sit between the two, the status bar is always last.
			status := ""
			for _, l := range s.Lines[i+1:] {
				if strings.TrimSpace(l) != "" {
					status = l
				}
			}
			return status
		}
	}
	return ""
}

// InputBox reports whether the bordered input box is on screen.
func (s Snapshot) InputBox() bool {
	var top, bottom bool
	for _, l := range s.Lines {
		t := strings.TrimSpace(l)
		top = top || strings.HasPrefix(t, "╭")
		bottom = bottom || (top && strings.HasPrefix(t, "╰"))
	}
	return top && bottom
}

// String renders the snapshot for a failure message.
func (s Snapshot) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "---- %dx%d cursor=(%d,%d) links=%d scrollback=%d lines (last 40 shown)\n",
		s.Cols, s.Rows, s.CursorX, s.CursorY, len(s.Links), len(s.Scrollback))
	back := s.Scrollback
	if len(back) > 40 {
		back = back[len(back)-40:]
	}
	for _, l := range back {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("---- screen\n")
	for _, l := range s.Lines {
		b.WriteString("| " + l + "\n")
	}
	return b.String()
}
