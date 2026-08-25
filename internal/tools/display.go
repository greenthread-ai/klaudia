package tools

import (
	"strconv"
	"strings"
)

// Models sometimes escape their JSON twice.
//
// Observed in a real session: an AskUserQuestion arrived, correctly decoded, as
//
//	On \"ask whether they want to change the permission model at that point\" — which did you mean?
//
// The model had written \\\" and \\u2014 into its tool input, so one round of
// JSON decoding — which is all that is correct — leaves the escapes as literal
// characters. Nothing in Klaudia is doing this wrong; the text really does
// contain a backslash. It just looks broken to whoever is reading it.
//
// So short human-facing strings are repaired on the way out. The repair is
// deliberately narrow, because the alternative failure is worse than the bug:
// silently rewriting text is only acceptable where the text cannot legitimately
// contain what we are rewriting.

// maxRepairable bounds what counts as a "short display string". A question or a
// one-line summary is fine to touch; a plan or a diff is not, and length is the
// cheapest way to keep them apart.
const maxRepairable = 2000

// unescapeDisplayText undoes a second round of escaping in a short, single-line
// display string.
//
// It only acts when the result parses cleanly as a quoted literal, so a string
// with a stray backslash is returned untouched rather than mangled. It is not
// applied to plans, diffs or code: those legitimately contain backslashes and
// \n sequences, and "repairing" them would corrupt real content.
func unescapeDisplayText(s string) string {
	if !strings.Contains(s, `\`) || len(s) > maxRepairable {
		return s
	}
	// A genuine newline means this is a block of text, not a label; blocks are
	// where real escape sequences live.
	if strings.ContainsAny(s, "\n\r") {
		return s
	}
	// An unescaped quote would make the literal unparseable anyway, but check
	// first so the intent is explicit rather than incidental.
	if strings.Contains(strings.ReplaceAll(s, `\"`, ""), `"`) {
		return s
	}
	out, err := strconv.Unquote(`"` + s + `"`)
	if err != nil {
		return s // not over-escaped, or not safely repairable
	}
	return out
}
