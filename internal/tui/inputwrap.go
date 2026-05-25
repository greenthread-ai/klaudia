package tui

import (
	"strings"
	"unicode"

	rw "github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// wrappedRowCount reports how many display rows the prompt textarea needs to
// render value at the given content width. bubbles' textarea soft-wraps each
// logical line to its width but exposes no total-height accessor (LineCount
// counts logical lines only), so we reproduce its wrap to size the input
// exactly — otherwise a long, word-wrapped line is sized to one row and the
// wrapped remainder scrolls out of view instead of growing the box.
//
// taWrap/taRepeatSpaces below are a verbatim port of textarea.wrap /
// repeatSpaces (charmbracelet/bubbles v1.0.0). Keep them in sync with upstream;
// if its wrap algorithm changes, the reserved height could drift by a row.
func wrappedRowCount(value string, width int) int {
	if width <= 0 {
		return strings.Count(value, "\n") + 1
	}
	rows := 0
	for _, line := range strings.Split(value, "\n") {
		rows += len(taWrap([]rune(line), width))
	}
	return rows
}

// taWrap is a verbatim port of textarea.wrap (bubbles v1.0.0).
func taWrap(runes []rune, width int) [][]rune {
	var (
		lines  = [][]rune{{}}
		word   = []rune{}
		row    int
		spaces int
	)

	// Word wrap the runes
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}

		if spaces > 0 { //nolint:nestif
			if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces > width {
				row++
				lines = append(lines, []rune{})
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], taRepeatSpaces(spaces)...)
				spaces = 0
				word = nil
			} else {
				lines[row] = append(lines[row], word...)
				lines[row] = append(lines[row], taRepeatSpaces(spaces)...)
				spaces = 0
				word = nil
			}
		} else {
			// If the last character is a double-width rune, then we may not be able to add it to this line
			// as it might cause us to go past the width.
			lastCharLen := rw.RuneWidth(word[len(word)-1])
			if uniseg.StringWidth(string(word))+lastCharLen > width {
				// If the current line has any content, let's move to the next
				// line because the current word fills up the entire line.
				if len(lines[row]) > 0 {
					row++
					lines = append(lines, []rune{})
				}
				lines[row] = append(lines[row], word...)
				word = nil
			}
		}
	}

	if uniseg.StringWidth(string(lines[row]))+uniseg.StringWidth(string(word))+spaces >= width {
		lines = append(lines, []rune{})
		lines[row+1] = append(lines[row+1], word...)
		// We add an extra space at the end of the line to account for the
		// trailing space at the end of the previous soft-wrapped lines so that
		// behaviour when navigating is consistent and so that we don't need to
		// continually add edges to handle the last line of the wrapped input.
		spaces++
		lines[row+1] = append(lines[row+1], taRepeatSpaces(spaces)...)
	} else {
		lines[row] = append(lines[row], word...)
		spaces++
		lines[row] = append(lines[row], taRepeatSpaces(spaces)...)
	}

	return lines
}

func taRepeatSpaces(n int) []rune {
	return []rune(strings.Repeat(string(' '), n))
}
