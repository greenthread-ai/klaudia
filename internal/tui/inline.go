package tui

import (
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// inlineRenderer draws the live region — the input box, status line and
// whatever sits around them — under the terminal's own scrollback, in place of
// Bubble Tea's standard renderer.
//
// Why not Bubble Tea's. Both of its inline renderers track where the cursor
// is in rows and move back by that count, and a resize invalidates the count:
// a terminal that rewraps turns one line into several rows, so the cursor
// stops inside the old frame and the rows above it are stranded. Measured
// with the e2e emulator. v1's renderer, with Klaudia predicting the rewrap: a
// drag with resizes 10–40ms apart left stacked input boxes (it draws on a
// 60fps tick, so the frame the prediction used was not the one on screen), a
// faster drag erased history rows, and on a terminal that does not rewrap at
// all (xterm) every narrowing erased the visible conversation. v2's cell
// renderer, unaided, strands a box on the first fast narrowing, and loses an
// editor's output across tea.ExecProcess.
//
// This renderer parks the cursor at column 0 of the frame's *first* line
// instead. Every terminal keeps the cursor in its cell across a resize —
// rewrapping or not, and whatever it does with the rows below — so after any
// resize the top of the old frame is exactly where the cursor is, and a
// redraw is "\r, erase below, draw". Nothing is predicted, so there is
// nothing to get wrong, and no frame needs to be known to be on screen.
//
// Every line is kept narrower than the terminal (a line that fills the last
// column leaves the cursor in the pending-wrap state, and a line wider than
// the terminal wraps into rows the move back up would not count), and the
// frame shorter than it, so the move back to the top is exact. Flushes are
// wrapped in synchronized-output marks (DEC mode 2026) so terminals that
// support them show each frame whole; the rest ignore the mode.
type inlineRenderer struct {
	out io.Writer

	mu        sync.Mutex
	width     int
	height    int
	frame     []string // wanted
	drawn     []string // on screen; nil when nothing is
	everDrawn bool     // whether the cursor is at a frame top yet
	full      bool     // the next flush redraws every line
	queued    []string // scrollback lines to print above the frame
	dirty     bool
	suspended bool

	done chan struct{}
	wg   sync.WaitGroup
}

const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
	// The frame is drawn with autowrap off (DECAWM). A frame drawn for the
	// old width can land just after the terminal narrowed — output is in
	// flight while the window is dragged — and with autowrap on its lines
	// would wrap into rows the move back to the top does not count. Off, an
	// over-wide line is clipped for the instant until the next frame, and
	// every line is exactly one row. Scrollback is printed with it on, so
	// long lines still wrap and the terminal can rewrap them later.
	wrapOff = "\x1b[?7l"
	wrapOn  = "\x1b[?7h"
	// rendererFPS matches Bubble Tea's default: streaming text arrives far
	// faster than anyone reads it, and each flush is a terminal write.
	rendererFPS = 60
)

func newInlineRenderer(out io.Writer) *inlineRenderer {
	return &inlineRenderer{out: out, done: make(chan struct{})}
}

// Terminal modes Bubble Tea's renderer would set, and does not while its
// output is muted: a hidden cursor (the input box draws its own), bracketed
// paste, and key disambiguation — modifyOtherKeys and the Kitty keyboard
// protocol, which are what let Shift+Enter arrive as itself rather than as
// Enter. Terminals ignore whichever they do not support; one that supports
// the Kitty protocol answers the query, and Bubble Tea turns the answer into
// the KeyboardEnhancementsMsg that lets the help name Shift+Enter.
var (
	modesOn  = ansi.HideCursor + ansi.SetModeBracketedPaste + ansi.SetModifyOtherKeys2 + ansi.PushKittyKeyboard(1) + ansi.RequestKittyKeyboard
	modesOff = ansi.ResetModifyOtherKeys + ansi.PopKittyKeyboard(1) + ansi.ResetModeBracketedPaste + ansi.ShowCursor
)

// start sets the terminal modes and begins flushing.
func (r *inlineRenderer) start() {
	r.write(modesOn)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		t := time.NewTicker(time.Second / rendererFPS)
		defer t.Stop()
		for {
			select {
			case <-r.done:
				return
			case <-t.C:
				r.flush()
			}
		}
	}()
}

// stop draws the last frame — empty when quitting, which erases the live
// region — and hands the terminal back.
func (r *inlineRenderer) stop() {
	select {
	case <-r.done:
		return
	default:
		close(r.done)
	}
	r.wg.Wait()
	r.flush()
	r.write(modesOff)
}

// suspend prints what is queued, erases the live region and gives the
// terminal to another program (a pager) until resume. Printing first keeps
// "Opening main.go…" above whatever the program leaves behind.
func (r *inlineRenderer) suspend() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frame, r.dirty = nil, true
	r.flushLocked()
	r.writeLocked(modesOff)
	r.drawn, r.suspended = nil, true
}

// resume takes the terminal back. The program that ran left the cursor
// wherever it finished, which is where the frame goes next.
func (r *inlineRenderer) resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeLocked(modesOn)
	r.suspended, r.full, r.dirty = false, true, true
}

// setFrame records the live region to draw next.
func (r *inlineRenderer) setFrame(view string) {
	var lines []string
	if view != "" {
		lines = strings.Split(downsample(view), "\n")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full && slices.Equal(lines, r.frame) {
		return
	}
	r.frame, r.dirty = lines, true
}

// resize takes the new size. The cursor is still at the top of the old
// frame, so the next flush simply redraws from there.
func (r *inlineRenderer) resize(w, h int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.width, r.height = w, h
	r.full, r.dirty = true, true
}

// print queues finished output for the scrollback above the frame.
func (r *inlineRenderer) print(body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queued = append(r.queued, strings.Split(downsample(body), "\n")...)
	r.dirty = true
}

// clearScreen erases the visible screen (not the scrollback) and starts the
// frame at the top.
func (r *inlineRenderer) clearScreen() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeLocked(ansi.EraseEntireScreen + ansi.CursorHomePosition)
	r.drawn, r.everDrawn = nil, true
	r.full, r.dirty = true, true
}

// flush writes what changed. On entry and on exit the cursor is at column 0
// of the frame's first line.
func (r *inlineRenderer) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushLocked()
}

func (r *inlineRenderer) flushLocked() {
	if !r.dirty || r.suspended {
		return
	}
	r.dirty = false
	lines := r.fit(r.frame)

	var b strings.Builder
	b.WriteString(syncBegin + "\r")
	moved := 0 // rows below the frame's first line the cursor ends on
	if r.full || len(r.queued) > 0 || r.drawn == nil {
		// Redraw from the top: the old frame, however the terminal rewrapped
		// it, is everything from the cursor down.
		if r.everDrawn {
			b.WriteString(ansi.EraseScreenBelow)
		}
		// Nothing to erase after these: the screen below was just cleared,
		// and an EraseLineRight after a line that fills the last column would
		// take its last character with it.
		for _, q := range r.queued {
			b.WriteString(q + "\r\n")
		}
		b.WriteString(wrapOff)
		for i, l := range lines {
			if i > 0 {
				b.WriteString("\r\n")
			}
			b.WriteString(l + ansi.EraseLineRight)
		}
		moved = len(lines) - 1
	} else {
		// Same size, nothing printed: rewrite only the lines that changed.
		b.WriteString(wrapOff)
		for i, l := range lines {
			if i > 0 {
				b.WriteString("\r\n")
			}
			if i < len(r.drawn) && r.drawn[i] == l {
				continue
			}
			b.WriteString(l + ansi.EraseLineRight)
		}
		moved = len(lines) - 1
		if len(r.drawn) > len(lines) {
			if len(lines) > 0 {
				b.WriteString("\r\n")
				moved++
			}
			b.WriteString(ansi.EraseScreenBelow)
		}
	}
	b.WriteString("\r")
	if moved > 0 {
		b.WriteString(ansi.CursorUp(moved))
	}
	b.WriteString(wrapOn + syncEnd)

	r.writeLocked(b.String())
	r.drawn, r.queued, r.full, r.everDrawn = lines, nil, false, true
}

// fit keeps every line narrower than the terminal and the frame shorter than
// it, which is what makes the cursor's way back to the top exact. The model
// already wraps the live region (fitLiveRegion, clampBottom); this is the
// guarantee, for widths too small for that to try.
func (r *inlineRenderer) fit(frame []string) []string {
	lines := frame
	if r.height > 1 && len(lines) > r.height-1 {
		lines = lines[len(lines)-(r.height-1):]
	}
	if r.width <= 1 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if ansi.StringWidth(l) >= r.width {
			l = ansi.Truncate(l, r.width-1, "")
		}
		out[i] = l
	}
	return out
}

func (r *inlineRenderer) write(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeLocked(s)
}

func (r *inlineRenderer) writeLocked(s string) { _, _ = io.WriteString(r.out, s) }

// mutedTTY is the output Bubble Tea is given. Its renderer still runs — with
// tea.WithoutRenderer, Bubble Tea also skips raw mode and resize signals —
// but what it writes is dropped, while Fd keeps the terminal's size and
// SIGWINCH reaching the program. Methods are spelled out rather than
// embedded so no promoted WriteString or ReadFrom can bypass Write.
type mutedTTY struct{ f *os.File }

func (t mutedTTY) Read(p []byte) (int, error) { return t.f.Read(p) }
func (mutedTTY) Write(p []byte) (int, error)  { return len(p), nil }
func (mutedTTY) Close() error                 { return nil }
func (t mutedTTY) Fd() uintptr                { return t.f.Fd() }

// ttyExec runs a command on the real terminal. Bubble Tea hands an exec'd
// program its own output, which is muted here; a pager given that would see
// a pipe, not a terminal, and print nothing.
type ttyExec struct{ cmd *exec.Cmd }

func (c ttyExec) Run() error           { return c.cmd.Run() }
func (c ttyExec) SetStdin(r io.Reader) { c.cmd.Stdin = r }
func (c ttyExec) SetStdout(io.Writer)  { c.cmd.Stdout = os.Stdout }
func (c ttyExec) SetStderr(io.Writer)  { c.cmd.Stderr = os.Stderr }

// rendered is the program Bubble Tea runs: the Model, with its frames sent
// to the inline renderer rather than returned to Bubble Tea's.
type rendered struct {
	m *Model
	r *inlineRenderer
}

func (p rendered) Init() tea.Cmd { return p.m.Init() }

func (p rendered) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		p.r.resize(ws.Width, ws.Height)
	}
	_, cmd := p.m.Update(msg)
	return p, cmd
}

func (p rendered) View() tea.View {
	p.r.setFrame(p.m.frame())
	return tea.View{}
}

// execProcess runs cmd with the terminal handed over, the live region erased
// first and redrawn after.
func (m *Model) execProcess(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
	r := m.screen
	if r == nil {
		return tea.ExecProcess(cmd, fn)
	}
	run := tea.Exec(ttyExec{cmd}, func(err error) tea.Msg {
		r.resume()
		return fn(err)
	})
	return func() tea.Msg {
		// What this Update queued is drained by a sibling command that may
		// not have run yet; it belongs above the program's output.
		if body, ok := m.out.drainText(); ok {
			r.print(body)
		}
		r.suspend()
		return run()
	}
}

// clearScreen is /clear's erase of the visible screen.
func (m *Model) clearScreen() tea.Cmd {
	r := m.screen
	if r == nil {
		return tea.ClearScreen
	}
	return func() tea.Msg {
		r.clearScreen()
		return nil
	}
}
