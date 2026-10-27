//go:build darwin || linux

package e2e

import (
	"os"
	"syscall"
	"unsafe"

	"github.com/mattn/go-runewidth"
)

// Resize changes the screen's size the way a terminal does when its window
// is dragged. With reflow, which is iTerm2, Terminal.app, kitty, Ghostty,
// WezTerm, Alacritty, VTE and tmux, soft-wrapped rows are joined into their
// logical lines and wrapped again at the new width, scrollback included; the
// cursor keeps its place in its line; and the screen shows the rows ending at
// the cursor or the last content, pulling rows back from scrollback when it
// grows. Without reflow, which is xterm, rows are cut or padded where they
// are and nothing rewraps.
func (s *Screen) Resize(cols, rows int, reflow bool) {
	if reflow {
		s.resizeReflow(cols, rows)
	} else {
		s.resizeClip(cols, rows)
	}
	s.cols, s.rows = cols, rows
}

func (s *Screen) resizeReflow(cols, rows int) {
	// The content ends at the cursor or the last non-blank row, whichever is
	// lower; blank rows below both are not content and are dropped.
	last := s.y
	for y := s.rows - 1; y > last; y-- {
		if s.rowText(s.grid[y]) != "" {
			last = y
			break
		}
	}
	all := append(append([]row(nil), s.scrollback...), s.grid[:last+1]...)
	cursorAt := len(s.scrollback) + s.y

	// Join rows into logical lines, noting the cursor's line and offset.
	var lines [][]string
	var cur []string
	cursorLine, cursorOff := 0, 0
	for i, r := range all {
		cells := r.cells
		if !r.wrapped {
			n := len(cells)
			for n > 0 && cells[n-1] == " " {
				n--
			}
			cells = cells[:n]
		}
		if i == cursorAt {
			cursorLine, cursorOff = len(lines), len(cur)+min(s.x, s.cols-1)
		}
		cur = append(cur, cells...)
		if !r.wrapped {
			lines = append(lines, cur)
			cur = nil
		}
	}
	if cur != nil {
		lines = append(lines, cur)
	}

	// Wrap each line at the new width.
	var out []row
	crow, ccol := 0, 0
	for li, cells := range lines {
		if li == cursorLine {
			for len(cells) <= cursorOff {
				cells = append(cells, " ") // the cursor sits past the text
			}
		}
		r := newRow(cols)
		x := 0
		for i := 0; i < len(cells); i++ {
			c := cells[i]
			if c == "" {
				continue // right half of a wide rune, re-made below
			}
			w := 1
			if rs := []rune(c); len(rs) > 0 {
				w = max(1, runewidth.RuneWidth(rs[0]))
			}
			if x+w > cols {
				r.wrapped = true
				out = append(out, r)
				r, x = newRow(cols), 0
			}
			if li == cursorLine && i == cursorOff {
				crow, ccol = len(out), x
			}
			r.cells[x] = c
			if w == 2 && x+1 < cols {
				r.cells[x+1] = ""
			}
			x += w
		}
		out = append(out, r)
	}

	start := max(0, len(out)-rows)
	s.scrollback = out[:start]
	s.grid = out[start:]
	for len(s.grid) < rows {
		s.grid = append(s.grid, newRow(cols))
	}
	s.y, s.x = crow-start, ccol
	s.savedX, s.savedY = min(s.savedX, cols-1), min(s.savedY, rows-1)
}

func (s *Screen) resizeClip(cols, rows int) {
	for i := range s.grid {
		s.grid[i] = clipRow(s.grid[i], cols)
	}
	// Shrinking drops blank rows below the cursor first, then scrolls the
	// top away; growing adds blank rows at the bottom.
	for len(s.grid) > rows && len(s.grid)-1 > s.y && s.rowText(s.grid[len(s.grid)-1]) == "" {
		s.grid = s.grid[:len(s.grid)-1]
	}
	if extra := len(s.grid) - rows; extra > 0 {
		s.scrollback = append(s.scrollback, s.grid[:extra]...)
		s.grid = s.grid[extra:]
		s.y -= extra
	}
	for len(s.grid) < rows {
		s.grid = append(s.grid, newRow(cols))
	}
	s.x = min(s.x, cols)
	s.savedX, s.savedY = min(s.savedX, cols-1), min(s.savedY, rows-1)
}

func newRow(cols int) row {
	r := make([]string, cols)
	for i := range r {
		r[i] = " "
	}
	return row{cells: r}
}

func clipRow(r row, cols int) row {
	n := newRow(cols)
	copy(n.cells, r.cells)
	return n
}

// Resize resizes the terminal: the emulator reflows first, as a real one
// does before the program hears about it, then the pty's size changes and the
// kernel sends the program SIGWINCH.
func (tm *Term) Resize(cols, rows int) {
	tm.mu.Lock()
	tm.screen.Resize(cols, rows, !tm.noReflow)
	tm.mu.Unlock()
	// Set on the slave: macOS refuses TIOCSWINSZ on the master.
	slave, err := os.OpenFile(tm.slaveName, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		tm.t.Fatalf("open %s: %v", tm.slaveName, err)
	}
	defer slave.Close()
	ws := winsize{rows: uint16(rows), cols: uint16(cols)}
	if err := ioctl(int(slave.Fd()), syscall.TIOCSWINSZ, unsafe.Pointer(&ws)); err != nil {
		tm.t.Fatalf("set window size: %v", err)
	}
}
