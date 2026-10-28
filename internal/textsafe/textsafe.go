// Package textsafe cleans text that reaches the model from files and servers
// Klaudia does not write itself — CLAUDE.md, memory and knowledge notes, MCP
// tool descriptions — so that what the model reads is what a person reading
// the same file would see.
package textsafe

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// StripInvisible removes Unicode format characters (category Cf): zero-width
// spaces, bidi overrides and isolates, the byte-order mark, and the tag
// characters U+E0000–U+E007F, which spell out ASCII that renders as nothing.
// Text carrying instructions in those characters passes a human review of the
// file and still reaches the model. ZWJ and ZWNJ are kept: emoji sequences
// and several scripts need them, and they carry no text of their own.
func StripInvisible(s string) string {
	if !strings.ContainsFunc(s, invisible) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, s)
}

func invisible(r rune) bool {
	return r != '\u200c' && r != '\u200d' && unicode.Is(unicode.Cf, r)
}

// Truncate cuts s to at most n bytes on a rune boundary, adding marker when it
// cuts.
func Truncate(s string, n int, marker string) string {
	if len(s) <= n {
		return s
	}
	cut := max(n-len(marker), 0)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + marker
}
