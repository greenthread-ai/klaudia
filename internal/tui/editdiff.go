package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// editHunk renders an Edit as a hunk with line numbers and a line of context
// either side, the way a reviewer reads a change:
//
//	12    func count(text string) map[string]int {
//	13 -      for _, w := range strings.Split(text, " ") {
//	13 +      for _, w := range words(text) {
//	14            out[w]++
//
// It used to print old_string as "-" lines and new_string as "+" lines, which
// shows a one-word change in a ten-line block as twenty changed lines with no
// idea where in the file they are. Lines the two strings share are context
// now, and the numbers come from finding the change in the file — new_string
// after the edit, old_string before it (an approval card shows the hunk before
// the edit runs). When neither is found, the hunk is drawn without numbers.
func (m *Model) editHunk(path, oldS, newS string) string {
	oldL, newL := strings.Split(oldS, "\n"), strings.Split(newS, "\n")

	// Lines the two strings share at either end are not part of the change.
	pre := 0
	for pre < len(oldL) && pre < len(newL) && oldL[pre] == newL[pre] {
		pre++
	}
	suf := 0
	for suf < len(oldL)-pre && suf < len(newL)-pre && oldL[len(oldL)-1-suf] == newL[len(newL)-1-suf] {
		suf++
	}

	// start: the 1-based line the strings begin on, or 0; span: how many
	// lines of the file they cover as found (old before the edit, new after).
	file, start, span := m.locateEdit(path, oldS, newS)
	var fileLines []string
	if file != "" {
		fileLines = strings.Split(file, "\n")
	}

	type row struct {
		n    int // 0 when unknown
		mark string
		text string
	}
	var rows []row
	num := func(i int) int {
		if start == 0 {
			return 0
		}
		return start + i
	}
	// One line of context before: the last shared line, else the file's line
	// above the strings.
	switch {
	case pre > 0:
		rows = append(rows, row{num(pre - 1), " ", oldL[pre-1]})
	case start > 1 && start-2 < len(fileLines):
		rows = append(rows, row{start - 1, " ", fileLines[start-2]})
	}
	for i := pre; i < len(oldL)-suf; i++ {
		rows = append(rows, row{num(i), "-", oldL[i]})
	}
	for i := pre; i < len(newL)-suf; i++ {
		rows = append(rows, row{num(i), "+", newL[i]})
	}
	// And one after, in the file's (new) numbering.
	switch {
	case suf > 0:
		i := len(newL) - suf
		rows = append(rows, row{num(i), " ", newL[i]})
	case start > 0 && start-1+span < len(fileLines):
		rows = append(rows, row{start + span, " ", fileLines[start-1+span]})
	}
	if len(rows) == 0 {
		return ""
	}

	width := 0
	for _, r := range rows {
		if w := len(fmt.Sprint(r.n)); r.n > 0 && w > width {
			width = w
		}
	}
	var out []string
	for i, r := range rows {
		if i >= maxDiffRows {
			out = append(out, "  "+bannerStyle.Render("…"))
			break
		}
		n := ""
		if width > 0 {
			n = strings.Repeat(" ", width)
			if r.n > 0 {
				n = fmt.Sprintf("%*d", width, r.n)
			}
			n += " "
		}
		var st lipgloss.Style
		switch r.mark {
		case "-":
			st = diffDelStyle
		case "+":
			st = diffAddStyle
		default:
			st = hintStyle
		}
		out = append(out, "  "+st.Render(n+r.mark+" "+clipLine(expandTabs(r.text, 4), maxDiffLine)))
	}
	return strings.Join(out, "\n")
}

// locateEdit reads the edited file and finds where the edit's strings start:
// new_string if the edit has happened, old_string if it has not. It returns
// the file's text, the 1-based line, and how many lines the found string
// spans — or 0s when it is in neither form.
func (m *Model) locateEdit(path, oldS, newS string) (string, int, int) {
	if path == "" {
		return "", 0, 0
	}
	if !filepath.IsAbs(path) {
		root := m.rootDir()
		if root == "" {
			return "", 0, 0
		}
		path = filepath.Join(root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 4<<20 {
		return "", 0, 0
	}
	file := string(data)
	for _, s := range []string{newS, oldS} {
		if s == "" {
			continue
		}
		if i := strings.Index(file, s); i >= 0 {
			return file, strings.Count(file[:i], "\n") + 1, strings.Count(s, "\n") + 1
		}
	}
	return "", 0, 0
}

// clipLine truncates to n runes, keeping the indentation: oneline collapses
// whitespace, which is right for a summary and wrong for code.
func clipLine(s string, n int) string {
	s = strings.TrimRight(s, " \t\r")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}
