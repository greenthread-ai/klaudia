package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// The @-completion menu.
//
// Tab completion existed, but nothing said so: typing "@" produced no visible
// change, so the feature was invisible unless you already knew it was there
// (reported as "@ doesn't work"). Slash commands have had a live type-ahead
// line since the beginning — this is the same promise kept for paths, as a
// list rather than one line because paths are long and a ranked list is the
// point.
//
// The menu is a VIEW over the Tab cycle rather than a second mechanism. Tab
// still inserts and advances exactly as it did; the list shows what is being
// cycled through and what comes next. That keeps Return unambiguous — the text
// is already in the input, so Return always means send, never "accept the
// highlighted item", which is the thing that makes editor-style menus
// infuriating in a prompt.
type pathMenu struct {
	open bool
	// stem is the partial the hits were computed for; recomputing is keyed on
	// it so arrow keys and spinner ticks don't re-rank the index.
	stem string
	hits []string
	idx  int
	// inserted is the exact input value the menu last wrote. A refresh that
	// sees it knows the change was ours and leaves the cycle alone.
	inserted string
	// dismissed is the stem Esc was pressed on, so the menu stays shut while
	// the user keeps typing that same reference.
	dismissed string
	quoted    bool
}

// pathMenuRows caps the visible entries. Eight is the slash line's budget and
// leaves room for the prompt box on a short terminal.
const pathMenuRows = 8

// atToken is an unfinished "@…" reference at the end of the input.
type atToken struct {
	at     int    // byte index of the '@'
	stem   string // the path fragment after it, unquoted
	quoted bool   // the user opened a quote: @"My Doc…
	suffix string // a :line[:col] tail, preserved across completion
}

// parseAtToken finds the open @reference the cursor is in, if any.
//
// The '@' must start a word. Without that rule the menu popped up inside every
// email address and Go module path typed into the prompt.
func parseAtToken(value string) (atToken, bool) {
	at := strings.LastIndex(value, "@")
	if at < 0 {
		return atToken{}, false
	}
	if at > 0 {
		prev, _ := lastRune(value[:at])
		if !unicode.IsSpace(prev) {
			return atToken{}, false
		}
	}
	rest := value[at+1:]
	tok := atToken{at: at}
	if strings.HasPrefix(rest, `"`) {
		// A quoted reference holds spaces, so only the closing quote ends it.
		if strings.Contains(rest[1:], `"`) {
			return atToken{}, false
		}
		tok.quoted = true
		tok.stem = rest[1:]
	} else {
		if strings.ContainsAny(rest, " \t") {
			return atToken{}, false // already closed by whitespace
		}
		tok.stem = rest
	}
	stem, line, col := splitLineSuffix(tok.stem)
	if line > 0 {
		tok.stem = stem
		tok.suffix = ":" + strconv.Itoa(line)
		if col > 0 {
			tok.suffix += ":" + strconv.Itoa(col)
		}
	}
	return tok, true
}

func lastRune(s string) (rune, bool) {
	rs := []rune(s)
	if len(rs) == 0 {
		return 0, false
	}
	return rs[len(rs)-1], true
}

// refreshPathMenu recomputes the menu from the current input. Called from the
// key handler, never from View: ranking the path index on every spinner tick
// would be a per-frame sort of up to 50k entries.
func (m *Model) refreshPathMenu() {
	value := m.input.Value()
	if m.menu.open && value == m.menu.inserted {
		return // our own insertion; the cycle continues
	}
	tok, ok := parseAtToken(value)
	if !ok {
		m.menu = pathMenu{}
		return
	}
	if m.menu.dismissed != "" && tok.stem == m.menu.dismissed {
		return
	}
	if m.menu.open && tok.stem == m.menu.stem && tok.quoted == m.menu.quoted {
		return // same partial, same hits — keep the selection
	}
	hits := m.matchPaths(tok.stem)
	m.menu = pathMenu{open: len(hits) > 0, stem: tok.stem, hits: hits, quoted: tok.quoted}
}

// dismissPathMenu closes the menu until the reference being typed changes.
func (m *Model) dismissPathMenu() bool {
	if !m.menuActive() {
		return false
	}
	m.menu = pathMenu{dismissed: m.menu.stem}
	return true
}

// movePathMenu steps the selection and writes it into the input, so what is
// highlighted and what will be sent are never two different things.
func (m *Model) movePathMenu(delta int) bool {
	if !m.menuActive() {
		return false
	}
	n := len(m.menu.hits)
	m.menu.idx = ((m.menu.idx+delta)%n + n) % n
	m.insertPathHit()
	return true
}

// insertPathHit replaces the open @token with the selected hit.
func (m *Model) insertPathHit() {
	value := m.input.Value()
	tok, ok := parseAtToken(value)
	if !ok || len(m.menu.hits) == 0 {
		return
	}
	completed := value[:tok.at] + "@" + quoteRef(m.menu.hits[m.menu.idx]) + tok.suffix
	m.menu.inserted = completed
	m.input.SetValue(completed)
	m.input.CursorEnd()
	m.syncInputHeight()
}

// quoteRef wraps a path in quotes when it contains whitespace, so the reference
// has an end the model (and the next Tab) can find. The quoted form is what the
// model receives, which is also how a filename holding U+202F survives being
// read back out of the prompt.
func quoteRef(path string) string {
	if strings.ContainsFunc(path, unicode.IsSpace) {
		return `"` + path + `"`
	}
	return path
}

// menuActive reports whether the dropdown still belongs on screen. The input
// can be replaced without going through the key path — history recall, a
// submit, a slash command — so the menu re-checks the line it is describing
// rather than trusting a flag set earlier.
func (m *Model) menuActive() bool {
	if !m.editableInput() || !m.menu.open || len(m.menu.hits) == 0 {
		return false
	}
	if m.input.Value() == m.menu.inserted {
		return true // our own insertion, which may have closed the token
	}
	_, ok := parseAtToken(m.input.Value())
	return ok
}

// pathMenuView renders the dropdown: the selected row marked, the rest plain,
// and a count when the list is longer than the window.
func (m *Model) pathMenuView() string {
	if !m.menuActive() {
		return ""
	}
	// Window the list around the selection so cycling past the eighth entry
	// doesn't walk off the bottom of a list that never scrolls.
	rows := min(pathMenuRows, len(m.menu.hits))
	start := 0
	if m.menu.idx >= rows {
		start = m.menu.idx - rows + 1
	}
	width := m.width - 6
	if width < 20 {
		width = 20
	}

	var b strings.Builder
	for i := start; i < start+rows; i++ {
		hit := ansi.Truncate(m.menu.hits[i], width, "…")
		if i == m.menu.idx {
			b.WriteString(caption(suggestStyle.Render("❯ " + hit)))
		} else {
			b.WriteString(caption(bannerStyle.Render("  " + hit)))
		}
		b.WriteString("\n")
	}
	hint := "tab/↑↓ to pick · esc to dismiss"
	if len(m.menu.hits) > rows {
		hint = strconv.Itoa(m.menu.idx+1) + "/" + strconv.Itoa(len(m.menu.hits)) + "  " + hint
	}
	b.WriteString(caption(hintStyle.Render("  " + hint)))
	return b.String()
}

// matchFilesystemPaths completes a reference that points outside the project:
// anything starting with /, ~ or ./. The cached index only covers the working
// directory, so "@~/Desktop/Screen…" matched nothing at all — which is how a
// screenshot path came to be typed by hand, with the wrong kind of space in it.
func matchFilesystemPaths(stem string) ([]string, bool) {
	expanded, ok := expandTilde(stem)
	if !ok && !strings.HasPrefix(stem, "/") && !strings.HasPrefix(stem, "./") && !strings.HasPrefix(stem, "../") {
		return nil, false
	}
	dir, base := filepath.Split(expanded)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, true // it is a filesystem reference; it just has no hits yet
	}
	type scored struct {
		path  string
		score int
	}
	var out []scored
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		score, ok := fuzzyScore(base, name)
		if !ok {
			continue
		}
		full := filepath.Join(dir, name)
		if e.IsDir() {
			full += string(filepath.Separator)
		}
		out = append(out, scored{full, score})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	paths := make([]string, 0, len(out))
	for _, s := range out {
		paths = append(paths, undoTilde(s.path, stem))
	}
	return paths, true
}

// expandTilde turns a leading ~ into the home directory.
func expandTilde(p string) (string, bool) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, false
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/")), true
}

// undoTilde puts the ~ back, so a reference typed as ~/… stays short and keeps
// meaning the same thing on another machine.
func undoTilde(full, stem string) string {
	if !strings.HasPrefix(stem, "~") {
		return full
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return full
	}
	rel, err := filepath.Rel(home, full)
	if err != nil || strings.HasPrefix(rel, "..") {
		return full
	}
	suffix := ""
	if strings.HasSuffix(full, string(filepath.Separator)) {
		suffix = string(filepath.Separator)
	}
	return "~" + string(filepath.Separator) + rel + suffix
}
