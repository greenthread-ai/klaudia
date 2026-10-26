package tui

import (
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/x/ansi"
)

// Clickable output: OSC 8 hyperlinks on URLs and on paths to files that exist.
//
// Inline rendering gave up the mouse on purpose (docs/ux-spec.md, Phase 1):
// the terminal owns selection, scroll and search, and a mouse handler would
// take all three back. OSC 8 keeps that bargain. The terminal still owns the
// click — Cmd/Ctrl+click opens the target — and Klaudia only says what the
// target is.
//
// Two things the terminal cannot work out for itself are why this exists.
// Glamour word-wraps a long URL across rows ("https://example." / "com/a/..."),
// and the terminal's own URL detection then opens the first fragment. And a
// bare relative path ("internal/tui/tui.go:3303") means nothing to the
// terminal, which does not know Klaudia's working directory.
//
// Only text that is visible is linked. Glamour prints a Markdown link as
// "label url", and the url is what gets the hyperlink, never the label — so the
// thing the user clicks always shows where it goes, however the model chose
// to word the label.
//
// Links are added to what goes to the screen and nowhere else. The transcript
// keeps the plain rendering, because /copy, /export and $PAGER read it and a
// pager that does not understand OSC 8 prints it as junk.

// HyperlinksEnabled decides whether the terminal gets OSC 8 links. setting is
// the config's links.enabled (nil = detect), and getenv is os.Getenv.
//
// KLAUDIA_HYPERLINKS (or FORCE_HYPERLINK, the variable the supports-hyperlinks
// convention uses) overrides both: "0" or "false" turns links off, any other
// value turns them on.
//
// Detection is an allowlist for the same reason Claude Code's is: a terminal
// that does not know OSC 8 usually ignores it, but "usually" includes some
// that print the URL inline, and an unreadable transcript is a worse failure
// than an unclickable one.
func HyperlinksEnabled(setting *bool, getenv func(string) string) bool {
	for _, k := range []string{"KLAUDIA_HYPERLINKS", "FORCE_HYPERLINK"} {
		if v := getenv(k); v != "" {
			return v != "0" && !strings.EqualFold(v, "false")
		}
	}
	if setting != nil {
		return *setting
	}
	if getenv("TERM") == "dumb" {
		return false
	}
	switch getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "Hyper", "Tabby", "rio", "WarpTerminal":
		return true
	}
	// These survive into tmux and over ssh, where TERM_PROGRAM is replaced.
	// tmux 3.4+ forwards OSC 8 to a terminal that supports it, and an older
	// tmux drops the sequence rather than printing it.
	if getenv("LC_TERMINAL") == "iTerm2" {
		return true
	}
	for _, k := range []string{"KITTY_WINDOW_ID", "WEZTERM_EXECUTABLE", "GHOSTTY_RESOURCES_DIR", "WT_SESSION", "KONSOLE_VERSION"} {
		if getenv(k) != "" {
			return true
		}
	}
	if v, err := strconv.Atoi(getenv("VTE_VERSION")); err == nil && v >= 5000 {
		return true
	}
	term := getenv("TERM")
	for _, t := range []string{"kitty", "ghostty", "wezterm", "foot", "alacritty"} {
		if strings.Contains(term, t) {
			return true
		}
	}
	return false
}

// linkSeq numbers links for the OSC 8 id parameter. A link wrapped across rows
// is written as one segment per row, and a shared id is what makes the
// terminal underline and open them as one link.
var linkSeq atomic.Uint64

// urlRe finds URLs in source text. It is deliberately greedy and the trailing
// punctuation is trimmed afterwards, which handles "(see https://x.y/z)." and
// a Markdown destination's closing paren without a lookbehind.
var urlRe = regexp.MustCompile("(?:https?|file)://[^\\s<>\"'`]+")

// pathRe finds tokens that might be file paths, with an optional :line or
// :line:col. Most matches are ordinary words; existence on disk decides.
var pathRe = regexp.MustCompile(`[A-Za-z0-9_.~@+/-]*[A-Za-z0-9_~@+-](?::(\d+))?(?::(\d+))?`)

// maxPathProbes bounds the stat calls one block can cost. A large tool result
// is mostly words with dots in them, and none of it is worth a slow commit.
const maxPathProbes = 64

type linkTarget struct {
	text string // as it appears in the output
	url  string
}

// linkTargets extracts what is worth linking from a block's source text:
// every URL, then every path that names an existing regular file. cwd anchors
// relative paths and fileFmt shapes file links (see fileLinkURL).
func linkTargets(src, cwd, fileFmt string) []linkTarget {
	var out []linkTarget
	seen := map[string]bool{}
	add := func(text, u string) {
		if !seen[text] {
			seen[text] = true
			out = append(out, linkTarget{text: text, url: u})
		}
	}
	for _, loc := range urlRe.FindAllStringIndex(src, -1) {
		u := trimURL(src[loc[0]:loc[1]])
		if len(u) > len("https://") && printable(u) {
			add(u, u)
		}
	}
	// Mask the URLs so their path components are not probed as files.
	masked := urlRe.ReplaceAllStringFunc(src, func(s string) string { return strings.Repeat(" ", len(s)) })
	exists := map[string]bool{} // by path, so a file named twice costs one stat
	for _, m := range pathRe.FindAllStringSubmatchIndex(masked, -1) {
		tok := masked[m[0]:m[1]]
		path := tok
		if m[2] >= 0 {
			path = masked[m[0] : m[2]-1]
		}
		path = strings.TrimRight(path, ".")
		if !strings.Contains(path, "/") && !strings.Contains(path, ".") {
			continue
		}
		abs := resolvePath(path, cwd)
		ok, probed := exists[abs]
		if !probed {
			if len(exists) >= maxPathProbes {
				break
			}
			fi, err := os.Stat(abs)
			ok = err == nil && fi.Mode().IsRegular()
			exists[abs] = ok
		}
		if !ok {
			continue
		}
		line, col := "", ""
		if m[2] >= 0 {
			line = masked[m[2]:m[3]]
		}
		if m[4] >= 0 {
			col = masked[m[4]:m[5]]
		}
		text := path
		if line != "" && tok[len(path):len(path)+1] == ":" {
			text = tok
		}
		add(text, fileLinkURL(fileFmt, abs, line, col))
	}
	// Longest first, so "a/b.go:12" claims its cells before "b.go" can.
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].text) > len(out[j].text) })
	return out
}

// trimURL drops the punctuation that ends the sentence a URL sits in, and a
// closing bracket that has no opening partner inside the URL.
func trimURL(u string) string {
	for u != "" {
		c := u[len(u)-1]
		switch {
		case strings.IndexByte(".,;:!?*_'\"", c) >= 0:
			u = u[:len(u)-1]
		case c == ')' && strings.Count(u, "(") < strings.Count(u, ")"):
			u = u[:len(u)-1]
		case c == ']' && strings.Count(u, "[") < strings.Count(u, "]"):
			u = u[:len(u)-1]
		default:
			return u
		}
	}
	return u
}

// printable reports whether s is safe inside an OSC string: no C0 controls
// and no DEL, either of which could end the sequence early.
func printable(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return false
		}
	}
	return true
}

func resolvePath(p, cwd string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	if filepath.IsAbs(p) || cwd == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(cwd, p)
}

// fileLinkURL builds the target for a file link. An empty format gives a
// file:// URL, which the terminal hands to the OS — and the OS knows nothing
// of line numbers. A format such as "vscode://file/{path}:{line}:{col}" sends
// the click to an editor at the right line instead; {line} and {col} default
// to 1 when the output did not name one.
func fileLinkURL(format, abs, line, col string) string {
	if format == "" {
		return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
	}
	if line == "" {
		line = "1"
	}
	if col == "" {
		col = "1"
	}
	return strings.NewReplacer("{path}", filepath.ToSlash(abs), "{line}", line, "{col}", col).Replace(format)
}

// linkify wraps every occurrence of each target in rendered with OSC 8.
//
// Matching runs over the visible text, so styling between characters does not
// break a match, and a match may cross a line break plus the whitespace either
// side of it — glamour's padding before the break and indentation after it —
// because that is exactly what a wrapped URL looks like. A match is written as
// one link segment per row, so no link is ever open across a newline: the
// renderer erases the rest of each row, and erased cells must not be
// clickable.
func linkify(rendered string, targets []linkTarget) string {
	if len(targets) == 0 || rendered == "" {
		return rendered
	}
	// vis is the visible text; at[i] is the byte in rendered that vis[i] came
	// from.
	vis := make([]byte, 0, len(rendered))
	at := make([]int, 0, len(rendered))
	for i := 0; i < len(rendered); {
		if rendered[i] == 0x1b {
			i += ansiLen(rendered[i:])
			continue
		}
		vis = append(vis, rendered[i])
		at = append(at, i)
		i++
	}

	type span struct {
		a, b int // vis indices, inclusive
		open string
	}
	var spans []span
	taken := make([]bool, len(vis))
	for _, t := range targets {
		for start := 0; start < len(vis); {
			k := bytes.IndexByte(vis[start:], t.text[0])
			if k < 0 {
				break
			}
			i := start + k
			start = i + 1
			if i > 0 && isPathByte(vis[i-1]) || taken[i] {
				continue
			}
			segs, end, ok := matchWrapped(vis, i, t.text)
			if !ok || end < len(vis) && isPathByte(vis[end]) && !isURL(t.text) {
				continue
			}
			clash := false
			for _, s := range segs {
				for j := s[0]; j <= s[1]; j++ {
					clash = clash || taken[j]
				}
			}
			if clash {
				continue
			}
			open := "\x1b]8;id=k" + strconv.FormatUint(linkSeq.Add(1), 36) + ";" + t.url + "\x1b\\"
			for _, s := range segs {
				for j := s[0]; j <= s[1]; j++ {
					taken[j] = true
				}
				spans = append(spans, span{s[0], s[1], open})
			}
			start = end
		}
	}
	if len(spans) == 0 {
		return rendered
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })

	const closeLink = "\x1b]8;;\x1b\\"
	var out strings.Builder
	out.Grow(len(rendered) + len(spans)*64)
	prev := 0
	for _, s := range spans {
		// After the last visible byte of the span, so a multi-byte rune is
		// never split by the close.
		from, to := at[s.a], at[s.b]+1
		out.WriteString(rendered[prev:from])
		out.WriteString(s.open)
		out.WriteString(rendered[from:to])
		out.WriteString(closeLink)
		prev = to
	}
	out.WriteString(rendered[prev:])
	return out.String()
}

// matchWrapped matches text against vis starting at i, allowing a run of
// whitespace that contains a newline to stand between any two characters. It
// returns the matched ranges, one per row, and the index just past the match.
func matchWrapped(vis []byte, i int, text string) (segs [][2]int, end int, ok bool) {
	segStart, j, k := i, 0, i
	for j < len(text) {
		if k < len(vis) && vis[k] == text[j] {
			j++
			k++
			continue
		}
		if j == 0 || k >= len(vis) || (vis[k] != ' ' && vis[k] != '\n') {
			return nil, 0, false
		}
		r, nl := k, false
		for r < len(vis) && (vis[r] == ' ' || vis[r] == '\n') {
			nl = nl || vis[r] == '\n'
			r++
		}
		if !nl {
			return nil, 0, false
		}
		segs = append(segs, [2]int{segStart, k - 1})
		segStart, k = r, r
	}
	return append(segs, [2]int{segStart, k - 1}), k, true
}

func isPathByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		strings.IndexByte("_/~@+-", c) >= 0
}

func isURL(s string) bool { return strings.Contains(s, "://") }

// linkify adds hyperlinks to a block on its way to the screen. A no-op unless
// the terminal supports them.
func (m *Model) linkify(rendered, source string) string {
	if m.sess == nil || !m.sess.Hyperlinks {
		return rendered
	}
	return linkify(rendered, linkTargets(ansi.Strip(source), m.sess.CWD, m.sess.FileLinks))
}
