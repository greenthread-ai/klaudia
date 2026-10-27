package tui

import (
	"regexp"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// markdown renders s through glamour, falling back to the raw text on error or
// before the renderer is built.
func (m *Model) markdown(s string) string {
	if m.glam == nil {
		return s
	}
	var parts []string
	for _, seg := range splitTables(s) {
		r := m.glam
		if seg.table > 0 && seg.table+2 < m.glamWidth {
			r = m.tableRenderer(seg.table + 2)
		}
		out, err := r.Render(seg.text)
		if err != nil {
			out = seg.text
		}
		if out = trimBlankLines(stripOSC8(out)); out != "" {
			parts = append(parts, out)
		}
	}
	return strings.Join(parts, "\n\n")
}

// osc8 matches an OSC 8 hyperlink open or close, BEL- or ST-terminated.
var osc8 = regexp.MustCompile("\x1b\\]8;[^\x07\x1b]*(?:\x07|\x1b\\\\)")

// stripOSC8 removes the hyperlinks glamour v2 writes round every Markdown
// link, unconditionally. Klaudia links scrollback itself (linkify), after
// deciding whether the terminal supports links at all (/doctor's row);
// glamour's would make that decision moot where the answer is no, and
// doubled links where it is yes.
func stripOSC8(s string) string {
	if !strings.Contains(s, "\x1b]8;") {
		return s
	}
	return osc8.ReplaceAllString(s, "")
}

// mdSegment is a run of Markdown; table is the content width a table segment
// needs, or 0 for anything else.
type mdSegment struct {
	text  string
	table int
}

// splitTables cuts Markdown into table and non-table runs, so each table can
// be rendered at its own width. glamour sizes every table to the full wrap
// width, so a two-column table of short cells spread across 100 columns with
// the second column a long way from the first. Fenced code is never split.
func splitTables(s string) []mdSegment {
	lines := strings.Split(s, "\n")
	var out []mdSegment
	var cur []string
	flush := func(table int) {
		if len(cur) > 0 {
			out = append(out, mdSegment{strings.Join(cur, "\n"), table})
			cur = nil
		}
	}
	inFence := false
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		if t := strings.TrimSpace(ln); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
		}
		if !inFence && isTableRow(ln) && i+1 < len(lines) && isTableDivider(lines[i+1]) {
			flush(0)
			j := i
			for j < len(lines) && isTableRow(lines[j]) {
				j++
			}
			cur = lines[i:j]
			flush(tableWidth(lines[i:j]))
			i = j - 1
			continue
		}
		cur = append(cur, ln)
	}
	flush(0)
	return out
}

func isTableRow(ln string) bool {
	t := strings.TrimSpace(ln)
	return strings.HasPrefix(t, "|") && strings.Count(t, "|") >= 2
}

func isTableDivider(ln string) bool {
	t := strings.TrimSpace(ln)
	return isTableRow(t) && strings.Trim(t, "|-: ") == ""
}

// tableWidth estimates the columns a table needs: each column's widest cell,
// with inline markup removed, plus glamour's padding and borders.
func tableWidth(rows []string) int {
	var widths []int
	for _, r := range rows {
		if isTableDivider(r) {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(r), "|"), "|")
		for c, cell := range cells {
			w := lipgloss.Width(strings.NewReplacer("`", "", "**", "", "__", "").Replace(strings.TrimSpace(cell)))
			if c >= len(widths) {
				widths = append(widths, 0)
			}
			if w > widths[c] {
				widths[c] = w
			}
		}
	}
	total := 0
	for _, w := range widths {
		total += w + 3 // a space either side and a border
	}
	return total + 1
}

// tableRenderer returns a renderer wrapping at width, built once per width
// and dropped with the main renderer (theme or size change).
func (m *Model) tableRenderer(width int) *glamour.TermRenderer {
	if r, ok := m.glamTables[width]; ok {
		return r
	}
	r, err := glamour.NewTermRenderer(
		m.glamourThemeOption(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return m.glam
	}
	if m.glamTables == nil {
		m.glamTables = map[int]*glamour.TermRenderer{}
	}
	m.glamTables[width] = r
	return r
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
	); err == nil {
		m.glam = r
		m.glamWidth = width
		m.glamTables = nil
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
//
// Until the terminal has reported its size, blocks are held rather than
// printed: fitScrollback cannot wrap to a width it does not know, so the
// startup notices went out unwrapped and the terminal split them mid-word.
// resize releases them.
func (m *Model) commit(b transcriptBlock) {
	m.flushReads() // a held run of Reads goes before whatever follows it
	b.rendered = trimRenderedPadding(b.rendered)
	m.transcript.add(b)
	if !m.ready {
		m.unsized = append(m.unsized, b)
		return
	}
	m.out.push(m.linkify(m.fitScrollback(b.rendered), b.text))
}

// fitScrollback hard-wraps a block so no line reaches the terminal's last
// column.
//
// Wrapping here rather than leaving it to the terminal breaks at words, with
// the continuation indented, where a terminal breaks mid-word at column 0
// (e2e TestLongLinesWrapAtWords). It began as a fix for Bubble Tea's renderer,
// which erased the rest of a queued line only when it was narrower than the
// terminal, leaving status-line residue off the end of wrapped rows;
// inlineRenderer erases below before printing, so that cannot recur. The
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
		out = append(out, wrapRow(line, limit)...)
	}
	return strings.Join(out, "\n")
}

// wrapRow splits one over-wide line into rows narrower than limit.
//
// At word boundaries, not mid-word: this only ever touches a line the terminal
// would have wrapped anyway, so it moves where the break falls, never the
// layout of a line that fits — and a hard wrap was splitting prose mid-word
// ("Klaudi|a's"). A token longer than a row (a path, a URL) still breaks hard.
// Continuation rows take the line's leading indent, so a wrapped tool result
// stays under its header instead of running back to column 0.
func wrapRow(line string, limit int) []string {
	plain := ansi.Strip(line)
	indent := plain[:len(plain)-len(strings.TrimLeft(plain, " "))]
	if len(indent) > limit/3 {
		indent = "" // a deep indent would leave too little room to be worth keeping
	}
	rows := strings.Split(ansi.Wrap(line, limit-len(indent), ""), "\n")
	for i := 1; i < len(rows); i++ {
		rows[i] = indent + rows[i]
	}
	return rows
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
