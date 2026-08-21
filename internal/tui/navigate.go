package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Conversation navigation.
//
// Rendering inline means Klaudia does not own the scroll region — the terminal
// does — so "jump to the previous error" cannot move the viewport, and
// pretending otherwise would be a worse lie than not offering it. These
// commands search and *report* instead: the answer is printed at the bottom,
// where it becomes part of scrollback like everything else. The terminal's own
// search (and tmux copy mode) already covers raw text; what it can't do is
// filter by "only my messages" or "only failed commands", which is what this
// index is for.

// completeCycle remembers an in-progress Tab cycle through @-completions.
// last is the exact input value we inserted: comparing against it is what tells
// a second Tab ("cycle to the next hit") apart from a first Tab on freshly
// typed text, since after an insertion the @token is the completion itself.
type completeCycle struct {
	base string
	last string
	hits []string
	idx  int
}

type navKind int

const (
	navUser navKind = iota
	navAssistant
	navCommand
	navError
)

func (k navKind) label() string {
	switch k {
	case navUser:
		return "you"
	case navAssistant:
		return "klaudia"
	case navCommand:
		return "tool"
	default:
		return "error"
	}
}

// navEntry is one searchable landmark in the conversation. Tool bodies are not
// duplicated here: ref points into the result ring instead.
type navEntry struct {
	seq   int
	kind  navKind
	at    time.Time
	title string
	body  string
	ref   int // resultRing seq, for tool entries
}

func (m *Model) noteNav(kind navKind, title, body string, ref int) {
	m.nav = append(m.nav, navEntry{
		seq: len(m.nav) + 1, kind: kind, at: time.Now(),
		title: oneline(title, 100), body: body, ref: ref,
	})
}

// searchBody is the text to match an entry against, resolving tool entries
// through the ring so their full output is searchable without storing it twice.
func (m *Model) searchBody(e navEntry) string {
	if e.ref > 0 {
		if res, ok := m.results.find(strconv.Itoa(e.ref)); ok {
			return res.content
		}
	}
	return e.body
}

// navFilter narrows /search to one kind of entry.
type navFilter struct {
	kinds map[navKind]bool
	label string
}

func parseNavFlags(args []string) (navFilter, []string) {
	f := navFilter{}
	var rest []string
	for _, a := range args {
		switch a {
		case "--mine", "--me":
			f.kinds, f.label = map[navKind]bool{navUser: true}, "your messages"
		case "--tools", "--out":
			f.kinds, f.label = map[navKind]bool{navCommand: true, navError: true}, "tool output"
		case "--errors":
			f.kinds, f.label = map[navKind]bool{navError: true}, "errors"
		case "--answers":
			f.kinds, f.label = map[navKind]bool{navAssistant: true}, "Klaudia's messages"
		default:
			rest = append(rest, a)
		}
	}
	return f, rest
}

func (f navFilter) allows(k navKind) bool {
	return f.kinds == nil || f.kinds[k]
}

// searchConversation backs /search. A query wrapped in slashes is a regex.
func (m *Model) searchConversation(args []string) tea.Cmd {
	filter, rest := parseNavFlags(args)
	query := strings.Join(rest, " ")
	if strings.TrimSpace(query) == "" && filter.kinds == nil {
		m.appendLine(errStyle.Render("usage: /search [--mine|--answers|--tools|--errors] <text or /regex/>"))
		return nil
	}

	var re *regexp.Regexp
	if len(query) > 2 && strings.HasPrefix(query, "/") && strings.HasSuffix(query, "/") {
		var err error
		if re, err = regexp.Compile("(?i)" + query[1:len(query)-1]); err != nil {
			m.appendLine(errStyle.Render("search: bad regex: " + err.Error()))
			return nil
		}
	}
	needle := strings.ToLower(query)

	var hits []string
	for _, e := range m.nav {
		if !filter.allows(e.kind) {
			continue
		}
		body := m.searchBody(e)
		line, ok := matchLine(body+"\n"+e.title, needle, re)
		if !ok {
			continue
		}
		hits = append(hits, fmt.Sprintf("  %3d  %-8s %s  %s",
			e.seq, e.kind.label(), e.at.Format("15:04:05"), oneline(line, 90)))
	}

	if len(hits) == 0 {
		what := "the conversation"
		if filter.label != "" {
			what = filter.label
		}
		m.appendLine(bannerStyle.Render(fmt.Sprintf("No match in %s.", what)))
		return nil
	}
	header := fmt.Sprintf("%d match(es) — /show <n> for the full entry:", len(hits))
	return m.showLong("search", header+"\n"+strings.Join(hits, "\n"))
}

// matchLine returns the first matching line of body. An empty needle with no
// regex matches everything, which is how the bare filters (/search --errors)
// work.
func matchLine(body, needle string, re *regexp.Regexp) (string, bool) {
	if needle == "" && re == nil {
		return firstLine(body), true
	}
	for _, ln := range strings.Split(body, "\n") {
		switch {
		case re != nil && re.MatchString(ln):
			return ln, true
		case re == nil && needle != "" && strings.Contains(strings.ToLower(ln), needle):
			return ln, true
		}
	}
	return "", false
}

// outline prints the shape of the session: what you asked, what ran, what
// failed. It is the "where did we get to" view for a long debugging session.
func (m *Model) outline() tea.Cmd {
	if len(m.nav) == 0 {
		m.appendLine(bannerStyle.Render("Nothing in this session yet."))
		return nil
	}
	var b strings.Builder
	b.WriteString("Session outline — /show <n> for the full entry:")
	for _, e := range m.nav {
		marker := map[navKind]string{
			navUser: "›", navAssistant: "·", navCommand: "⚙", navError: "✗",
		}[e.kind]
		indent := "    "
		if e.kind == navUser {
			indent = "  "
		}
		fmt.Fprintf(&b, "\n%s%3d %s %s  %s", indent, e.seq, marker, e.at.Format("15:04:05"), e.title)
	}
	return m.showLong("outline", b.String())
}

// showEntry backs /show <n>: the full text of one indexed entry.
func (m *Model) showEntry(args []string) tea.Cmd {
	if len(args) == 0 {
		m.appendLine(errStyle.Render("usage: /show <n>   (numbers come from /search or /outline)"))
		return nil
	}
	n, err := strconv.Atoi(args[0])
	if err != nil || n < 1 || n > len(m.nav) {
		m.appendLine(errStyle.Render(fmt.Sprintf("No entry %q. /outline lists them.", args[0])))
		return nil
	}
	e := m.nav[n-1]
	m.appendLine(bannerStyle.Render(fmt.Sprintf("#%d · %s · %s",
		e.seq, e.kind.label(), e.at.Format("15:04:05"))))
	return m.showLong("entry", m.searchBody(e))
}

// listErrors backs /errors — "jump to the last failure", reframed as a report
// because the app cannot move the terminal's scroll position.
func (m *Model) listErrors(args []string) tea.Cmd {
	limit := 10
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			limit = n
		}
	}
	var hits []string
	for i := len(m.nav) - 1; i >= 0 && len(hits) < limit; i-- {
		if e := m.nav[i]; e.kind == navError {
			hits = append(hits, fmt.Sprintf("  %3d  %s  %s",
				e.seq, e.at.Format("15:04:05"), e.title))
		}
	}
	if len(hits) == 0 {
		m.appendLine(bannerStyle.Render("No errors in this session."))
		return nil
	}
	// Reverse so the newest reads last, next to the prompt.
	for i, j := 0, len(hits)-1; i < j; i, j = i+1, j-1 {
		hits[i], hits[j] = hits[j], hits[i]
	}
	m.appendLine(bannerStyle.Render(fmt.Sprintf(
		"%d most recent error(s) — /show <n> for the full output:\n%s",
		len(hits), strings.Join(hits, "\n"))))
	return nil
}
