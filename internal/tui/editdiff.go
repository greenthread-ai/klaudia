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
//
// line is where the Edit tool reported the change starts, or 0. It wins over
// the file: the TUI reads the file when it renders, and by then the next call
// may be rewriting it (os.WriteFile truncates first), which left a hunk in a
// live run unnumbered. The file is then used only for context, and only if it
// agrees.
func (m *Model) editHunk(path, oldS, newS string, line int) string {
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
	if line > 0 {
		if start != line {
			file = "" // the file has moved on, or was caught mid-write
		}
		start, span = line, len(newL)
	}
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

// writePreview is the hunk an approval card shows for a Write. A new file is
// its first lines; an existing one is what changes in it, which is the whole
// question when the file is ~/.zshrc. Read before the write runs: the card is
// written while the call waits for the answer.
func (m *Model) writePreview(path, content string) string {
	if path != "" && !filepath.IsAbs(path) {
		if root := m.rootDir(); root != "" {
			path = filepath.Join(root, path)
		}
	}
	old, err := os.ReadFile(path)
	if err != nil || len(old) > 4<<20 {
		return addedSummary(content)
	}
	if string(old) == content {
		return "  " + hintStyle.Render("(no change)")
	}
	return lineDiff(string(old), content)
}

// maxDiffCells bounds lineDiff's table; beyond it the change is summarised.
const maxDiffCells = 4_000_000

// lineDiff renders the changed lines of old → new with a line of context and
// line numbers (the new file's for context and additions, the old file's for
// removals), capped at maxDiffRows, followed by the totals.
func lineDiff(oldS, newS string) string {
	a, b := strings.Split(oldS, "\n"), strings.Split(newS, "\n")
	if len(a)*len(b) > maxDiffCells {
		return "  " + hintStyle.Render(fmt.Sprintf("(replaces %s with %s)", countOf(len(a), "line"), countOf(len(b), "line")))
	}
	// lcs[i][j]: length of the longest common subsequence of a[i:], b[j:].
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	type op struct {
		mark string
		n    int
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			ops = append(ops, op{" ", j + 1, b[j]})
			i, j = i+1, j+1
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{"+", j + 1, b[j]})
			j++
		default:
			ops = append(ops, op{"-", i + 1, a[i]})
			i++
		}
	}
	// Keep changes and the line either side of each.
	keep := make([]bool, len(ops))
	added, removed := 0, 0
	for k, o := range ops {
		if o.mark == " " {
			continue
		}
		if o.mark == "+" {
			added++
		} else {
			removed++
		}
		for d := -1; d <= 1; d++ {
			if k+d >= 0 && k+d < len(ops) {
				keep[k+d] = true
			}
		}
	}
	width := len(fmt.Sprint(len(b)))
	if w := len(fmt.Sprint(len(a))); w > width {
		width = w
	}
	var out []string
	last := -1
	for k, o := range ops {
		if !keep[k] {
			continue
		}
		if len(out) >= maxDiffRows {
			out = append(out, "  "+bannerStyle.Render("…"))
			break
		}
		if last >= 0 && k > last+1 {
			out = append(out, "  "+hintStyle.Render(strings.Repeat(" ", width)+" ⋮"))
		}
		last = k
		st := hintStyle
		switch o.mark {
		case "-":
			st = diffDelStyle
		case "+":
			st = diffAddStyle
		}
		out = append(out, "  "+st.Render(fmt.Sprintf("%*d %s %s", width, o.n, o.mark, clipLine(expandTabs(o.text, 4), maxDiffLine))))
	}
	out = append(out, "  "+hintStyle.Render(fmt.Sprintf("+%d -%d", added, removed)))
	return strings.Join(out, "\n")
}
