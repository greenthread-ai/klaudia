package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Pasting is intercepted before the text ever reaches the textarea, because
// bubbles' widget runs every insertion through runeutil.NewSanitizer(), which
//
//   - replaces each tab with four spaces, and
//   - maps '\r'→"\n" and '\n'→"\n" *independently*, so CRLF text arrives
//     double-spaced (every line of a Windows-style paste gains a blank line).
//
// The sanitizer lives in an unexported field with no setter, and SetValue and
// InsertString both route through it, so it cannot be swapped or bypassed via
// the public API. Interception is the only way to keep a paste byte-exact.
//
// Anything large or tab-bearing is parked in a pasteStore and shown as a short
// chip; the payload is substituted back verbatim at submit. That also keeps a
// thousand-line paste from swamping a six-row input box.

const (
	// Pastes at or under these bounds are inserted inline, so the ordinary
	// "paste a line or two" case looks exactly as it always has.
	pasteInlineMaxLines = 3
	pasteInlineMaxBytes = 400
	// Total payload bytes retained per session before the oldest are evicted.
	pasteStoreMaxBytes = 8 << 20
)

// pasteChipRe matches a chip. The trailing summary is deliberately loose so a
// chip whose text the user has partially retyped still resolves by id.
var pasteChipRe = regexp.MustCompile(`\[#(\d+) pasted · [^\]]*\]`)

// pasteStore holds paste payloads verbatim, keyed by the id shown in the chip.
// It is session-scoped: history recall re-shows a chip, so entries must outlive
// the turn that created them.
type pasteStore struct {
	items map[int]string
	order []int // insertion order, for eviction
	next  int
	bytes int
}

// add stores text and returns the chip to display in its place.
func (p *pasteStore) add(text string) string {
	if p.items == nil {
		p.items = map[int]string{}
	}
	p.next++
	id := p.next
	p.items[id] = text
	p.order = append(p.order, id)
	p.bytes += len(text)
	p.evict()
	return fmt.Sprintf("[#%d pasted · %s]", id, pasteSummary(text))
}

// evict drops the oldest payloads once the store exceeds its byte budget. An
// evicted chip stops expanding and is submitted as its literal text — degraded
// but never silently wrong, and only after 8 MiB of pasting in one session.
func (p *pasteStore) evict() {
	for p.bytes > pasteStoreMaxBytes && len(p.order) > 1 {
		oldest := p.order[0]
		p.order = p.order[1:]
		p.bytes -= len(p.items[oldest])
		delete(p.items, oldest)
	}
}

// expand substitutes every resolvable chip in s with its stored payload.
// Unknown ids are left alone: if the user edited a chip beyond recognition, or
// it was evicted, we send what is actually on screen rather than guessing.
func (p *pasteStore) expand(s string) string {
	if len(p.items) == 0 || !strings.Contains(s, "[#") {
		return s
	}
	return pasteChipRe.ReplaceAllStringFunc(s, func(chip string) string {
		m := pasteChipRe.FindStringSubmatch(chip)
		if m == nil {
			return chip
		}
		id, err := strconv.Atoi(m[1])
		if err != nil {
			return chip
		}
		if text, ok := p.items[id]; ok {
			return text
		}
		return chip
	})
}

func (p *pasteStore) reset() { *p = pasteStore{} }

// pasteSummary describes a payload in the few words a chip has room for.
func pasteSummary(text string) string {
	if n := strings.Count(text, "\n") + 1; n > 1 {
		return fmt.Sprintf("%d lines", n)
	}
	return fmt.Sprintf("%d chars", len([]rune(text)))
}

// normalizeNewlines collapses CRLF and bare CR to LF. Without this the widget's
// sanitizer turns every CRLF into two newlines.
func normalizeNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// chipWorthy reports whether a paste must be parked rather than inserted
// inline: tabs would be destroyed by the sanitizer, and bulk text would make
// the input box unusable.
func chipWorthy(text string) bool {
	return strings.Contains(text, "\t") ||
		strings.Count(text, "\n")+1 > pasteInlineMaxLines ||
		len(text) > pasteInlineMaxBytes
}
