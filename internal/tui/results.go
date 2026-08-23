package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The TUI shows a short preview of each tool result and used to keep only the
// single most recent one in full, so as soon as any later tool ran, the output
// you actually wanted was gone. Nothing upstream truncates — agent.Event.Content
// is byte-identical to what the model saw — so keeping a bounded ring costs
// little and makes every recent result recoverable.

const (
	resultRingMaxItems = 200
	resultRingMaxBytes = 8 << 20
)

type toolResult struct {
	seq  int // 1-based, monotonic for the session; what /last takes
	id   string
	tool string
	// command is the Bash command line this result came from, when it was one.
	// The end-of-turn summary needs it to tell a test run from a build.
	command string
	isError bool
	at      time.Time
	content string // untruncated when the tool kept a fuller copy
	clamped bool   // the model saw a shorter version of this
}

type resultRing struct {
	items []toolResult
	bytes int
	seq   int
}

// since returns the results recorded after sequence number n, for the
// end-of-turn summary. The ring may have evicted some; what remains is what
// the summary can honestly describe.
func (r *resultRing) since(n int) []toolResult {
	var out []toolResult
	for _, it := range r.items {
		if it.seq > n {
			out = append(out, it)
		}
	}
	return out
}

// add stores a result and returns the sequence number the UI should advertise.
func (r *resultRing) add(res toolResult) int {
	r.seq++
	res.seq = r.seq
	r.items = append(r.items, res)
	r.bytes += len(res.content)
	// Evict oldest-first, but never the newest: a single result larger than the
	// whole budget should still be readable.
	for len(r.items) > 1 && (len(r.items) > resultRingMaxItems || r.bytes > resultRingMaxBytes) {
		r.bytes -= len(r.items[0].content)
		r.items = r.items[1:]
	}
	return res.seq
}

func (r *resultRing) latest() (toolResult, bool) {
	if len(r.items) == 0 {
		return toolResult{}, false
	}
	return r.items[len(r.items)-1], true
}

// find resolves a /last argument: a sequence number, or a tool_use_id prefix so
// an id copied out of a log works too.
func (r *resultRing) find(arg string) (toolResult, bool) {
	if n, err := strconv.Atoi(arg); err == nil {
		for _, it := range r.items {
			if it.seq == n {
				return it, true
			}
		}
		return toolResult{}, false
	}
	for i := len(r.items) - 1; i >= 0; i-- {
		if it := r.items[i]; it.id != "" && strings.HasPrefix(it.id, arg) {
			return it, true
		}
	}
	return toolResult{}, false
}

func (r *resultRing) reset() { *r = resultRing{} }

// index renders the "what do I have" listing, which doubles as an outline of
// everything the session ran.
func (r *resultRing) index() string {
	if len(r.items) == 0 {
		return "No tool output yet."
	}
	var b strings.Builder
	b.WriteString("Tool output held in this session (newest last):")
	for _, it := range r.items {
		mark := "✓"
		if it.isError {
			mark = "✗"
		}
		lines := strings.Count(it.content, "\n") + 1
		fmt.Fprintf(&b, "\n  %3d  %s %-14s %5d lines  %8s  %s",
			it.seq, mark, it.tool, lines, humanBytes(len(it.content)),
			oneline(firstLine(it.content), 48))
	}
	b.WriteString("\n\n/last <n> shows one in full.")
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// showResult backs /last. With no argument it shows the newest result; with a
// number or tool_use_id prefix, that one; with "list", the index.
func (m *Model) showResult(args []string) tea.Cmd {
	if len(args) > 0 && (args[0] == "list" || args[0] == "ls") {
		m.appendLine(bannerStyle.Render(m.results.index()))
		return nil
	}
	var (
		res toolResult
		ok  bool
	)
	if len(args) > 0 {
		if res, ok = m.results.find(args[0]); !ok {
			m.appendLine(errStyle.Render(fmt.Sprintf(
				"No tool output %q. Try /last list.", args[0])))
			return nil
		}
	} else if res, ok = m.results.latest(); !ok {
		m.appendLine(bannerStyle.Render("No tool output yet."))
		return nil
	}
	mark := "✓"
	if res.isError {
		mark = "✗"
	}
	header := fmt.Sprintf("%s %s · #%d · %s", mark, res.tool, res.seq, humanBytes(len(res.content)))
	if res.clamped {
		// Worth saying: what follows is more than the model was given.
		header += " (full output — the model saw a clamped copy)"
	}
	m.appendLine(bannerStyle.Render(header))
	return m.showLong(res.tool, res.content)
}
