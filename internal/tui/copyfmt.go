package tui

import "strings"

// Making rendered output safe to copy.
//
// Glamour pads every line of every document to the wrap width in order to paint
// block backgrounds — this is done by its margin/padding writers, not by the
// style config, so no StyleConfig field turns it off. On screen the padding is
// invisible; in the clipboard it is dozens of trailing spaces on every line,
// and combined with the code-block margin it means a copied snippet arrives
// indented and ragged. Since we can't stop glamour producing it, we strip it
// from the rendered string before the text ever reaches scrollback.

// trimRenderedPadding removes trailing whitespace from every line while keeping
// the ANSI escapes that follow it, so colours still reset correctly.
func trimRenderedPadding(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = trimLinePadding(ln)
	}
	return strings.Join(lines, "\n")
}

// trimLinePadding drops the trailing run of spaces from one line. Escape
// sequences inside the run are preserved, in their original order, because
// dropping a reset would bleed styling into the rest of the terminal.
func trimLinePadding(ln string) string {
	var out strings.Builder
	// held buffers everything seen since the last visible character: spaces and
	// escapes interleaved. It is committed verbatim when another visible
	// character arrives, so ordering within the line is never disturbed.
	var held []byte
	// escOnly mirrors held with the spaces removed, for the end-of-line case.
	var escOnly []byte

	for i := 0; i < len(ln); {
		if ln[i] == 0x1b {
			n := ansiLen(ln[i:])
			held = append(held, ln[i:i+n]...)
			escOnly = append(escOnly, ln[i:i+n]...)
			i += n
			continue
		}
		if ln[i] == ' ' || ln[i] == '\t' {
			held = append(held, ln[i])
			i++
			continue
		}
		out.Write(held)
		held, escOnly = held[:0], escOnly[:0]
		out.WriteByte(ln[i])
		i++
	}
	out.Write(escOnly) // keep resets, drop the padding they trailed
	return out.String()
}

// ansiLen returns the byte length of the escape sequence starting at s[0],
// which is assumed to be ESC. Unrecognised sequences count as one byte so the
// scanner can always make progress.
func ansiLen(s string) int {
	if len(s) < 2 {
		return 1
	}
	switch s[1] {
	case '[': // CSI: parameters then a final byte in @..~
		for i := 2; i < len(s); i++ {
			if s[i] >= '@' && s[i] <= '~' {
				return i + 1
			}
		}
		return len(s)
	case ']': // OSC: terminated by BEL or ST
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	default:
		return 2
	}
}

// clipPreview shortens a tool result for the inline preview, returning the
// clipped text and how many lines were dropped. It counts runes, not bytes: the
// previous byte slice cut multi-byte characters in half and printed replacement
// characters. It also respects line structure, because a flat rune budget over
// a fifty-line result produces an unreadable ribbon.
func clipPreview(s string, maxLines, maxRunes int) (string, int) {
	lines := strings.Split(s, "\n")
	dropped := 0
	if len(lines) > maxLines {
		dropped = len(lines) - maxLines
		lines = lines[:maxLines]
	}
	out := strings.Join(lines, "\n")
	if r := []rune(out); len(r) > maxRunes {
		out = string(r[:maxRunes])
		// The rune budget hid the rest of the last kept line too.
		if dropped == 0 {
			dropped = strings.Count(s[len(out):], "\n")
		}
	}
	return out, dropped
}
