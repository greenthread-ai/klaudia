package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

// The input is drawn inside a border.
//
// This is a fix for a complaint that looked like a layout problem and wasn't.
// A dim, full-width "model · mode · turns · tokens" line reads as a status bar,
// and status bars belong pinned to the bottom of a window — so when inline
// rendering left it sitting a few rows below the shell prompt, it looked
// broken. Two attempts at moving it down made things worse, because the
// position was never the issue: the line had no visible thing to belong to.
//
// Framing the input as a widget gives it one. The status line becomes a caption
// under the box you type into, which is true wherever on the screen it happens
// to be, so it stops looking misplaced without anyone having to pin it.

// Below these dimensions the two rows and two columns the border costs are
// worth more than the framing, so the input is drawn bare.
const (
	minBoxHeight = 10
	minBoxWidth  = 30
)

// promptGutter is the width reserved for the "› " marker on the first line.
const promptGutter = 2

// promptBoxStyle frames the input. Re-coloured by applyChromeTheme.
var promptBoxStyle = lipgloss.NewStyle()

// boxed reports whether the terminal has room for the framed input.
func (m *Model) boxed() bool {
	return m.height >= minBoxHeight && m.width >= minBoxWidth
}

// inputWidth is the textarea's content width. Five columns go to the border,
// its padding and the reserved last column when boxed, and to a matching margin
// when not — the same number either way, so crossing the threshold doesn't
// reflow what you typed.
func (m *Model) inputWidth() int {
	if w := m.width - 5; w >= 8 {
		return w
	}
	return 8
}

// promptBox renders the input, framed when there's room for it.
func (m *Model) promptBox() string {
	view := m.input.View()
	if !m.boxed() {
		return view
	}
	// width-3 content + 2 border columns = width-1 total, deliberately one
	// short of the terminal. Bubble Tea only appends EraseLineRight when a line
	// is *narrower* than the terminal (standard_renderer.go), so a line that
	// occupies the full width never erases what was to the right of it — and
	// writing the last column also parks the cursor in the terminal's
	// pending-wrap state. Measured: three of the four live-region lines used to
	// be exactly the terminal width.
	return promptBoxStyle.Width(m.width - 3).Render(view)
}

// captionStyle indents a line so it reads as a caption under the box rather
// than as a separate full-width element.
func caption(s string) string {
	if s == "" {
		return ""
	}
	return "  " + s
}

// newPromptInput builds the textarea. The "› " marker on the first line matches
// the marker the transcript uses for submitted prompts, so what you are typing
// and what you typed look like the same kind of thing.
func newPromptInput() textarea.Model {
	in := textarea.New()
	// Short enough to fit the box on a narrow terminal. The key hints live in
	// the intro banner and /help; a placeholder that gets clipped mid-word
	// teaches nothing.
	in.Placeholder = "Ask Klaudia…"
	in.Prompt = ""
	in.ShowLineNumbers = false
	in.EndOfBufferCharacter = ' '
	in.CharLimit = 0
	in.MaxHeight = 6
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j", "newline"))
	// Marker on the first line only: repeating it down a wrapped paste would
	// read as several prompts rather than one.
	in.SetPromptFunc(promptGutter, func(line int) string {
		if line == 0 {
			return "› "
		}
		return "  "
	})
	in.Focus()
	return in
}

// resizeResync: why the live region blanks for one frame after a resize.
//
// Bubble Tea's inline renderer returns to the top of the live region with
// CursorUp(linesRendered-1), where linesRendered is the *logical* line count of
// the previous frame (standard_renderer.go). On a resize it updates its width
// and height and calls repaint(), but it does not reset that count — and the
// terminal has meanwhile reflowed the rows already on screen. A four-line region
// drawn at 144 columns occupies seven or eight rows at 100, so the cursor lands
// inside the old frame, the new frame is painted from there, and the rows above
// it are orphaned. Drag a window and every intermediate size leaves another
// stripe: the stair-stepped border fragments in the bug report.
//
// Nothing in the public API resets that counter. What does reset it is a frame
// exactly one line tall: the renderer sets linesRendered to the new frame's
// length, and — because the previous frame was taller — also emits
// EraseScreenBelow, wiping the mispositioned remains. The frame after it starts
// from linesRendered == 1, so no CursorUp is emitted at all and it paints
// cleanly wherever the cursor now is.
//
// So a resize renders "" once and the real frame immediately after. The cost is
// one blank frame, which at a follow-up of well under a millisecond is not
// visible; the benefit is that the damage stops accumulating across a drag.
//
// The other half of the fix is geometric and lives in promptBox and
// statusLine: no line in the live region occupies the terminal's last column,
// so every line gets an EraseLineRight and the cursor never sits in the
// pending-wrap state.
type resyncMsg struct{}
