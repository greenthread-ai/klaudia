package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// markdown renders s through glamour, falling back to the raw text on error or
// before the renderer is built.
func (m *Model) markdown(s string) string {
	if m.glam == nil {
		return s
	}
	out, err := m.glam.Render(s)
	if err != nil {
		return s
	}
	return strings.TrimRight(out, "\n")
}

// looksLineNumbered reports whether s is cat -n style output — the Read tool's
// format of a right-aligned line number, a tab, then content ("%6d\t%s").
// Markdown-rendering such preformatted text reflows every line into one run-on
// block and inlines the numbers, so the caller must show it verbatim. Requires
// every checked non-empty line (the first few) to match, so a genuine Markdown
// result that merely happens to contain a numbered line isn't misclassified.
func looksLineNumbered(s string) bool {
	checked, numbered := 0, 0
	for _, ln := range strings.SplitN(s, "\n", 6) {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		checked++
		i := 0
		for i < len(ln) && ln[i] == ' ' {
			i++
		}
		d := i
		for d < len(ln) && ln[d] >= '0' && ln[d] <= '9' {
			d++
		}
		if d > i && d < len(ln) && ln[d] == '\t' {
			numbered++
		}
	}
	return checked > 0 && numbered == checked
}

// buildGlamour (re)builds the Markdown renderer for the given viewport width.
//
// We use a fixed "dark" style rather than WithAutoStyle: auto-style queries the
// terminal for its background colour (an OSC escape + reply), which blocks
// inside Bubble Tea's input loop — Bubble Tea owns stdin, so the reply never
// reaches glamour and it hangs until its internal timeout (the "initializing…"
// freeze). "dark" is the safe default for developer terminals.
func (m *Model) buildGlamour(width int) {
	w := width - 2
	if w < 20 {
		w = 20
	}
	// glamour hardcodes a TrueColor profile and ignores NO_COLOR; pass lipgloss's
	// detected profile (which honours NO_COLOR and CLICOLOR) so Markdown degrades
	// in step with the rest of the chrome.
	if r, err := glamour.NewTermRenderer(
		m.glamourThemeOption(),
		glamour.WithWordWrap(w),
		glamour.WithColorProfile(lipgloss.ColorProfile()),
	); err == nil {
		m.glam = r
		m.glamWidth = width
	}
}

// appendMarkdown commits a Markdown block to scrollback.
func (m *Model) appendMarkdown(s string) {
	m.commit(transcriptBlock{text: s, markdown: true, rendered: m.markdown(s)})
}

// appendLine commits a full, already-styled line to scrollback.
func (m *Model) appendLine(s string) {
	m.commit(transcriptBlock{text: s, rendered: s})
}

// commit records a block and queues it for printing. Once printed it is part of
// the terminal's scrollback and can never be revised — every caller should be
// sure the content is final.
//
// Trailing padding is stripped here, at the single choke point, because two
// different things add it: glamour pads every line to the wrap width to paint
// block backgrounds, and lipgloss pads a multi-line block out to its widest
// line. Both are invisible on screen and both are trailing whitespace on every
// line once the text is selected and pasted.
func (m *Model) commit(b transcriptBlock) {
	b.rendered = trimRenderedPadding(b.rendered)
	m.transcript.add(b)
	m.out.push(m.linkify(m.fitScrollback(b.rendered), b.text))
}

// fitScrollback hard-wraps a block so no line reaches the terminal's last
// column.
//
// Bubble Tea appends EraseLineRight to a queued scrollback line only when it is
// *narrower* than the terminal (standard_renderer.go). A longer line gets none —
// the terminal wraps it, and the short final row keeps whatever was on that row
// before. Seen in a real session: a 160-character prompt echoed at 149 columns
// left "0k tokens" from the status line hanging off the end of its second row,
// and the tail of an earlier prompt off another.
//
// Wrapping it ourselves turns every physical row into its own logical line, so
// each one gets erased. The terminal would have wrapped at the same points
// anyway; the difference is that these rows are now ours to clean up. The
// transcript keeps the unwrapped text, so /copy and /export are unaffected.
func (m *Model) fitScrollback(s string) string {
	// Tabs are expanded here and nowhere earlier. Two bugs need it. A literal
	// HT advances the cursor without painting the cells it skips, so a queued
	// scrollback line written over the previous frame's rows leaves that
	// frame's prompt border, placeholder and status bar showing through the
	// indentation in 8- and 16-column runs. And ansi.StringWidth scores a tab
	// as zero columns (HT is an ExecuteAction, not a PrintAction), so the
	// measurement below would call a tab-heavy line short, skip the wrap, and
	// hand the terminal a line it wraps onto rows we never erase.
	//
	// It happens at this choke point rather than in baseStyle so the two uses
	// stay separable: m.transcript keeps the literal tabs, which is what /copy
	// and /export read and the reason NoTabConversion is set at all.
	s = expandTabs(s, tabStop)
	limit := m.width - 1
	if limit < 20 || s == "" {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if ansi.StringWidth(line) < limit {
			out = append(out, line)
			continue
		}
		// Hardwrap rather than word-wrap: this is about physical rows, and a
		// word-aware break would reflow code and diffs that were laid out
		// deliberately.
		out = append(out, strings.Split(ansi.Hardwrap(line, limit, true), "\n")...)
	}
	return strings.Join(out, "\n")
}

// tabStop is the column interval the overwhelming majority of terminals use,
// and the one the bleed runs in the original report measured.
const tabStop = 8

// expandTabs replaces each HT with spaces up to the next tab stop, counting
// columns rather than bytes so the result lines up the way the terminal would
// have. lipgloss's own expansion writes a fixed four spaces regardless of
// column, which is what destroys the alignment of `go test` and kubectl output
// and the reason baseStyle disables it.
//
// Columns are measured with ansi.StringWidth on the run before each tab: escape
// sequences occupy no cells, and counting their bytes would push every later
// tab on the line to the wrong stop.
func expandTabs(s string, stop int) string {
	if stop <= 0 || !strings.ContainsRune(s, '\t') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + stop)
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		col := 0
		for {
			j := strings.IndexByte(line, '\t')
			if j < 0 {
				b.WriteString(line)
				break
			}
			b.WriteString(line[:j])
			col += ansi.StringWidth(line[:j])
			pad := stop - col%stop
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
			line = line[j+1:]
		}
	}
	return b.String()
}
