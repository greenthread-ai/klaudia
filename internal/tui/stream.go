package tui

import (
	"bytes"
	"strings"
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
