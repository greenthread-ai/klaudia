package tui

import (
	"bytes"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Progressive flushing of a streaming assistant message.
//
// Inline rendering makes scrollback immutable: once a line is printed it can
// never be restyled or re-laid-out. That rules out the obvious approach of
// re-rendering the whole message per token and printing the stable prefix,
// because Markdown layout is not monotonic — a later table row widens every
// earlier column, and an ordered list renumbers at 9→10. Anything we print
// early has to be something we will never want to revise.
//
// So we only flush at boundaries where the *following* block cannot change the
// preceding one, and we scan incrementally (a byte cursor, complete lines only)
// so the cost per token stays flat rather than quadratic in message length.

type blockKind int

const (
	kindOther blockKind = iota
	kindList
	kindQuote
	kindIndented
)

// streamScan tracks how much of a streaming message is safe to print.
// The zero value is ready to use.
type streamScan struct {
	scanned  int  // bytes consumed
	lines    int  // complete lines seen
	inFence  bool // inside a fenced code block
	fence    string
	safe     int // byte offset (exclusive) of the last flushable prefix
	prevKind blockKind
	cand     int // offset just past a blank line, awaiting the next block
	haveCand bool
}

// advance consumes any complete lines appended to buf since the last call. It
// takes the raw bytes rather than a string so that scanning a growing message
// stays O(bytes appended) instead of copying the whole buffer per token — the
// same quadratic trap this migration removed from the render path.
func (s *streamScan) advance(buf []byte) {
	for {
		nl := bytes.IndexByte(buf[s.scanned:], '\n')
		if nl < 0 {
			return // partial line; wait for more
		}
		end := s.scanned + nl
		s.consume(string(buf[s.scanned:end]), end+1)
		s.scanned = end + 1
		s.lines++
	}
}

// consume folds one complete line into the scan state. next is the offset just
// past the line's newline.
func (s *streamScan) consume(line string, next int) {
	if fence, closing := fenceToken(line); fence != "" {
		switch {
		case !s.inFence:
			// An opening fence after a blank line begins a new block, so the
			// blank line before it is a genuine boundary — resolve the pending
			// candidate before we stop looking at boundaries for the duration
			// of the block.
			s.settle(kindOther)
			s.inFence, s.fence = true, fence
		case closing && fence[0] == s.fence[0] && len(fence) >= len(s.fence):
			s.inFence, s.fence = false, ""
			// A closing fence ends the block; the next blank line is a
			// boundary like any other.
		}
		s.haveCand = false
		s.prevKind = kindOther
		return
	}
	if s.inFence {
		return // never split a code block
	}
	if strings.TrimSpace(line) == "" {
		if !s.haveCand {
			s.cand, s.haveCand = next, true
		}
		return
	}
	kind := classifyLine(line)
	s.settle(kind)
	s.prevKind = kind
}

// settle resolves a pending blank-line candidate now that we know what kind of
// block follows it. A blank line is only a real boundary if the next block does
// not continue the previous one: loose lists and blockquotes contain interior
// blank lines, and splitting them would restart list numbering or break the
// quote — permanently, since scrollback cannot be revised.
func (s *streamScan) settle(next blockKind) {
	if !s.haveCand {
		return
	}
	if !continues(s.prevKind, next) {
		s.safe = s.cand
	}
	s.haveCand = false
}

// rebase adjusts the offsets after the caller drops the first n bytes.
func (s *streamScan) rebase(n int) {
	s.scanned -= n
	if s.scanned < 0 {
		s.scanned = 0
	}
	s.safe -= n
	if s.safe < 0 {
		s.safe = 0
	}
	s.cand -= n
	if s.cand < 0 {
		s.cand, s.haveCand = 0, false
	}
}

func (s *streamScan) reset() { *s = streamScan{} }

// fenceToken returns the backtick/tilde run opening or closing a code fence,
// and whether the line is a bare fence (so it can close one).
func fenceToken(line string) (string, bool) {
	t := strings.TrimLeft(line, " ")
	if len(line)-len(t) > 3 || t == "" {
		return "", false
	}
	c := t[0]
	if c != '`' && c != '~' {
		return "", false
	}
	n := 0
	for n < len(t) && t[n] == c {
		n++
	}
	if n < 3 {
		return "", false
	}
	return t[:n], strings.TrimSpace(t[n:]) == ""
}

func classifyLine(line string) blockKind {
	t := strings.TrimLeft(line, " ")
	indent := len(line) - len(t)
	if indent >= 4 || strings.HasPrefix(line, "\t") {
		return kindIndented
	}
	if strings.HasPrefix(t, ">") {
		return kindQuote
	}
	if isBullet(t) || isOrdered(t) {
		return kindList
	}
	return kindOther
}

func isBullet(t string) bool {
	if len(t) < 2 {
		return false
	}
	return (t[0] == '-' || t[0] == '*' || t[0] == '+') && (t[1] == ' ' || t[1] == '\t')
}

func isOrdered(t string) bool {
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	if i == 0 || i+1 >= len(t) {
		return false
	}
	return (t[i] == '.' || t[i] == ')') && (t[i+1] == ' ' || t[i+1] == '\t')
}

// continues reports whether a block of kind cur is a continuation of one of
// kind prev, i.e. whether the blank line between them is structural rather than
// a boundary.
func continues(prev, cur blockKind) bool {
	if cur == kindIndented {
		return true // indented code or a list continuation paragraph
	}
	return prev == cur && (prev == kindList || prev == kindQuote)
}

// appendText buffers a streamed assistant delta (rendered raw live, then
// prettified through glamour on flush).
func (m *Model) appendText(s string) {
	m.streamBuf.WriteString(s)
	m.flushSafeChunks()
}

// streamFlushMin is how many complete lines a message must reach before we
// start committing it in pieces. Below it we hold everything and render once at
// the end, which keeps ordinary short answers byte-identical to a single
// glamour pass; above it, waiting would leave a long answer stuck in a
// few-row preview.
const streamFlushMin = 12

// streamTailLines caps the live preview of the unflushed remainder.
const streamTailLines = 6

// flushSafeChunks commits any prefix of the in-progress message that later
// tokens cannot change (see stream.go).
func (m *Model) flushSafeChunks() {
	m.scan.advance(m.streamBuf.bytes())
	if m.scan.lines < streamFlushMin || m.scan.safe <= 0 {
		return
	}
	n := m.scan.safe
	m.emitChunk(string(m.streamBuf.bytes()[:n]))
	m.streamBuf.trim(n)
	m.scan.rebase(n)
}

// emitChunk renders and commits one piece of an assistant message. Glamour puts
// a margin and a leading newline on every document it renders, so chunks are
// trimmed and the inter-chunk blank line is emitted deliberately — otherwise a
// chunked message would be spaced differently from a single-pass one.
func (m *Model) emitChunk(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m.msgAccum.WriteString(text)
	rendered := strings.Trim(m.markdown(text), "\n")
	if m.chunked {
		rendered = "\n" + rendered
	}
	m.chunked = true
	m.commit(transcriptBlock{text: text, markdown: true, rendered: rendered})
}

// truncateToWidth clips one line to w cells, preserving ANSI. The live region
// must never soft-wrap: Bubble Tea's inline renderer tracks it by line count,
// so a wrapped line desynchronises the cursor arithmetic and corrupts the frame.
func truncateToWidth(s string, w int) string {
	if w <= 0 {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(w).Render(s)
}

// streamTail is the live preview of the not-yet-committed remainder.
func (m *Model) streamTail() string {
	raw := strings.TrimRight(m.streamBuf.String(), "\n")
	if raw == "" {
		return ""
	}
	// Wrap, then keep the last rows. Truncating each line kept its *start*:
	// a one-paragraph reply is one long line, so the preview sat frozen on
	// its first row while the text arrived off its end. width-1, not width:
	// a preview line that fills the last column is the case that strands
	// the live region in scrollback (see fitLiveRegion).
	limit := m.width - 1
	var rows []string
	for _, ln := range strings.Split(raw, "\n") {
		if limit >= 20 && ansi.StringWidth(ln) > limit {
			rows = append(rows, strings.Split(ansi.Wrap(ln, limit, ""), "\n")...)
		} else {
			rows = append(rows, ln)
		}
	}
	if len(rows) > streamTailLines {
		rows = rows[len(rows)-streamTailLines:]
	}
	for i, ln := range rows {
		rows[i] = truncateToWidth(ln, limit) // a backstop for a narrow terminal
	}
	return toolStyle.Render(strings.Join(rows, "\n"))
}

// flushAssistant commits the buffered assistant message to the transcript,
// rendered as Markdown via glamour. No-op when nothing is buffered.
func (m *Model) flushAssistant() {
	if m.streamBuf.Len() > 0 {
		m.emitChunk(m.streamBuf.String())
		m.streamBuf.Reset()
	}
	if m.msgAccum.Len() > 0 {
		m.lastAssistantText = m.msgAccum.String()
		m.noteNav(navAssistant, firstLine(strings.TrimSpace(m.lastAssistantText)), m.lastAssistantText, 0)
		m.msgAccum.Reset()
	}
	m.scan.reset()
	m.chunked = false
}
