// Package fuzzy provides the ordered-subsequence scoring shared by the TUI's
// file completion and the agent's tool search. Both want the same underlying
// question answered — do these characters appear in order, and how tightly? —
// while ranking the answer differently, so the primitive lives here and the
// domain-specific tiers stay with their callers.
package fuzzy

// boundary reports whether c precedes a word start, for the purposes of
// rewarding matches that begin a segment rather than landing mid-word.
func boundary(c byte) bool {
	switch c {
	case '/', '.', '_', '-', ' ':
		return true
	}
	return false
}

// Subsequence matches pat against cand as an ordered subsequence, rewarding
// consecutive runs and matches at segment boundaries. It reports false when
// pat is not a subsequence of cand at all.
//
// Both inputs are expected to already be case-folded by the caller, which is
// what lets the TUI implement smart case (an uppercase rune in the pattern
// makes the match case-sensitive) without this function knowing about it.
func Subsequence(pat, cand string) (int, bool) {
	score, pi, prev := 200, 0, -2
	for ci := 0; ci < len(cand) && pi < len(pat); ci++ {
		if cand[ci] != pat[pi] {
			continue
		}
		if ci == prev+1 {
			score += 10 // consecutive
		}
		if ci == 0 || boundary(cand[ci-1]) {
			score += 15 // segment boundary
		}
		prev = ci
		pi++
	}
	if pi < len(pat) {
		return 0, false
	}
	return score, true
}
