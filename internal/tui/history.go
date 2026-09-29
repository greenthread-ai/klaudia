package tui

import (
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"
)

// MaxInputHistory caps the remembered prompts, in memory and on disk.
const MaxInputHistory = 200

// maxStoredPromptBytes is the largest entry written to the history file. A
// prompt bigger than this is almost always pasted material — a log, a diff —
// that has no business outliving the session in a second file, and would fill
// the box to its limit if recalled.
const maxStoredPromptBytes = 8 << 10

// PromptHistoryStore keeps ↑ history across sessions. Load returns the stored
// prompts oldest first; Append records one.
type PromptHistoryStore interface {
	Load() ([]string, error)
	Append(string) error
}

// loadInputHistory builds the starting ↑ history: the project's stored prompts,
// then the resumed conversation's prompts that are not already among them.
// Stored entries predate the session; a resumed conversation is what the user
// is about to continue, so its prompts are the most recent thing to recall.
func (m *Model) loadInputHistory(resumed []anthropic.BetaMessageParam) {
	var entries []string
	if m.sess != nil && m.sess.PromptHistory != nil {
		stored, err := m.sess.PromptHistory.Load()
		if err != nil {
			m.historyFault("read", err)
		}
		entries = stored
	}
	have := make(map[string]bool, len(entries))
	for _, e := range entries {
		have[e] = true
	}
	for _, p := range typedPrompts(resumed) {
		if !have[p] {
			have[p] = true
			entries = append(entries, p)
		}
	}
	if len(entries) > MaxInputHistory {
		entries = entries[len(entries)-MaxInputHistory:]
	}
	m.inputHistory = entries
	m.histPos = len(entries)
}

// storePrompt writes one newly-remembered prompt to the history file.
//
// A prompt carrying a paste chip is not stored: the chip's payload lives in
// this session's paste store only, so in the next session the entry would
// recall a placeholder that expands to nothing. Oversized entries are skipped
// for the reason on maxStoredPromptBytes. A `!` command is stored as the line
// that was typed; its output never reaches history.
func (m *Model) storePrompt(prompt string) {
	if m.sess == nil || m.sess.PromptHistory == nil {
		return
	}
	if len(prompt) > maxStoredPromptBytes || pasteChipRe.MatchString(prompt) {
		return
	}
	if err := m.sess.PromptHistory.Append(prompt); err != nil {
		m.historyFault("save", err)
	}
}

// historyFault reports the first history-file failure. History is a
// convenience, so it never stops anything — but a history that silently stops
// persisting looks exactly like one that works until the next restart.
func (m *Model) historyFault(verb string, err error) {
	if m.historyFaulted {
		return
	}
	m.historyFaulted = true
	m.appendLine(hintStyle.Render("  (could not " + verb + " prompt history: " + err.Error() + ")"))
}

// Framing that startTurn and the agent loop wrap around a typed prompt before
// it reaches the transcript. Seeding recovers the typed text from inside it.
const (
	standingGoalPrefix   = "Standing goal for this session: "
	standingGoalInstr    = "\n\nCurrent instruction: "
	facilitatorPrefix    = "You are helping the user define a clear goal specification"
	facilitatorUser      = "\n\nUser: "
	interjectionPrefix   = "The user interrupted with a new instruction."
	haltPrefix           = "The user asked you to stop after the current step."
	pinnedBlockPrefix    = "Files the user has pinned as important for this task."
	runningJobsPrefix    = "Background jobs still running from before this interruption:"
	shellContextPrefix   = "The user ran this in the terminal:"
	shellContextSentinel = "\n```\n(exit "
)

// generatedPrompts open turns Klaudia wrote rather than the user: goal-loop
// iterations, the compaction carry-forward, /logs --errors. None of them is
// something to recall with ↑.
var generatedPrompts = []string{
	"You are autonomously iterating toward the goal",
	"The goal loop is stopping before the goal is complete",
	"Before iterating on the goal, the Progress tracker",
	"You said the goal is complete.",
	"[Conversation compacted",
	"Summary of the earlier conversation",
	"Errors from the log of job ",
	haltPrefix,
}

// shellContextRe matches one leading `!` command block (bang.go
// recordShellContext) and the blank line that joins it to the prompt.
var shellContextRe = regexp.MustCompile("(?s)^" + regexp.QuoteMeta(shellContextPrefix) +
	"\n\n```\n.*?" + regexp.QuoteMeta(shellContextSentinel) + `-?\d+\)(\n\n|$)`)

// typedPrompts recovers what the user typed from a resumed conversation's user
// turns, oldest first. Tool results are skipped, and so are turns Klaudia wrote
// itself. It is heuristic by necessity — the transcript stores the framed
// prompt — so anything it cannot cleanly unwrap is left out rather than
// recalled with Klaudia's framing attached. In particular a `!` command's
// output, which rides in front of the next prompt, is never recalled.
func typedPrompts(history []anthropic.BetaMessageParam) []string {
	var out []string
	for _, msg := range history {
		if msg.Role != anthropic.BetaMessageParamRoleUser {
			continue
		}
		var texts []string
		toolTurn := false
		for _, c := range msg.Content {
			switch {
			case c.OfToolResult != nil:
				toolTurn = true
			case c.OfText != nil:
				texts = append(texts, c.OfText.Text)
			}
		}
		if toolTurn || len(texts) == 0 {
			continue
		}
		if p, ok := unwrapTypedPrompt(strings.Join(texts, "\n\n")); ok {
			if n := len(out); n == 0 || out[n-1] != p {
				out = append(out, p)
			}
		}
	}
	return out
}

// unwrapTypedPrompt strips the framing startTurn and the steer path add, in
// the order they nest, and reports whether what is left is a typed prompt.
func unwrapTypedPrompt(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, g := range generatedPrompts {
		if strings.HasPrefix(s, g) {
			return "", false
		}
	}
	switch {
	case strings.HasPrefix(s, standingGoalPrefix):
		i := strings.Index(s, standingGoalInstr)
		if i < 0 {
			return "", false
		}
		s = s[i+len(standingGoalInstr):]
	case strings.HasPrefix(s, facilitatorPrefix):
		i := strings.Index(s, facilitatorUser)
		if i < 0 {
			return "", false
		}
		s = s[i+len(facilitatorUser):]
	case strings.HasPrefix(s, interjectionPrefix):
		i := strings.Index(s, "\n\n")
		if i < 0 {
			return "", false
		}
		s = s[i+2:]
		if j := strings.Index(s, "\n\n"+haltPrefix); j >= 0 {
			s = s[:j]
		}
	}
	for {
		switch {
		case strings.HasPrefix(s, pinnedBlockPrefix), strings.HasPrefix(s, runningJobsPrefix):
			// Neither block contains a blank line, so the first one ends it.
			i := strings.Index(s, "\n\n")
			if i < 0 {
				return "", false
			}
			s = s[i+2:]
			continue
		case strings.HasPrefix(s, shellContextPrefix):
			loc := shellContextRe.FindStringIndex(s)
			if loc == nil {
				return "", false
			}
			s = s[loc[1]:]
			continue
		}
		break
	}
	s = strings.TrimSpace(s)
	// Belt and braces: if a block survived unwrapping, the entry would carry
	// command output (or other framing) into history. Drop it instead.
	if s == "" || len(s) > maxStoredPromptBytes || strings.Contains(s, shellContextPrefix) {
		return "", false
	}
	return s, true
}

// historySearch is the Ctrl+R reverse incremental search over inputHistory.
type historySearch struct {
	active bool
	query  string
	match  int    // index into inputHistory of the shown match; -1 for none
	failed bool   // the query matches nothing at or before the last match
	draft  string // the box before the search began, restored on cancel
}

// beginHistorySearch opens Ctrl+R search with an empty query.
func (m *Model) beginHistorySearch() {
	m.search = historySearch{active: true, match: -1, draft: m.input.Value()}
}

// findHistory returns the newest entry at or before from that contains query,
// or -1.
func (m *Model) findHistory(query string, from int) int {
	if from >= len(m.inputHistory) {
		from = len(m.inputHistory) - 1
	}
	for i := from; i >= 0; i-- {
		if strings.Contains(m.inputHistory[i], query) {
			return i
		}
	}
	return -1
}

// researchHistory re-runs the search for the current query starting at from,
// keeping the previous match on screen when nothing new matches (bash's
// "failing reverse-i-search").
func (m *Model) researchHistory(from int) {
	if m.search.query == "" {
		m.search.failed = false
		return
	}
	if i := m.findHistory(m.search.query, from); i >= 0 {
		m.search.match, m.search.failed = i, false
		m.input.SetValue(m.inputHistory[i])
		m.input.CursorEnd()
	} else {
		m.search.failed = true
	}
	m.syncInputHeight()
}

// endHistorySearch closes the search. Accepting leaves the match in the box to
// edit (not sent: a recalled prompt is usually the start of the next one) and
// points ↑/↓ at it, so browsing continues from there; cancelling restores what
// was in the box before.
func (m *Model) endHistorySearch(accept bool) {
	s := m.search
	m.search = historySearch{}
	if accept && s.match >= 0 {
		m.histPos = s.match
		m.histDraft = s.draft
		return
	}
	m.input.SetValue(s.draft)
	m.input.CursorEnd()
	m.syncInputHeight()
}

// onSearchKey handles a key while Ctrl+R search is open. It reports whether
// the key was consumed; a key it does not consume accepts the match and then
// gets its ordinary meaning (as in readline, where →, ↑ or Home leave the
// search and act on the found line).
func (m *Model) onSearchKey(msg tea.KeyMsg) bool {
	if msg.Paste {
		m.endHistorySearch(true)
		return false
	}
	switch msg.Type {
	case tea.KeyCtrlR:
		from := len(m.inputHistory) - 1
		if m.search.match >= 0 {
			from = m.search.match - 1
		}
		if m.search.query != "" && from >= 0 {
			m.researchHistory(from)
		}
		return true
	case tea.KeyEsc, tea.KeyCtrlG, tea.KeyCtrlC:
		m.endHistorySearch(false)
		return true
	case tea.KeyEnter, tea.KeyTab:
		m.endHistorySearch(true)
		return true
	case tea.KeyBackspace:
		if r := []rune(m.search.query); len(r) > 0 {
			m.search.query = string(r[:len(r)-1])
			if m.search.query == "" {
				m.search.match = -1
				m.input.SetValue(m.search.draft)
				m.input.CursorEnd()
				m.syncInputHeight()
			}
			m.researchHistory(len(m.inputHistory) - 1)
		}
		return true
	case tea.KeyRunes, tea.KeySpace:
		if msg.Alt {
			break
		}
		m.search.query += string(msg.Runes)
		if msg.Type == tea.KeySpace && len(msg.Runes) == 0 {
			m.search.query += " "
		}
		from := len(m.inputHistory) - 1
		if m.search.match >= 0 {
			from = m.search.match // a longer query may still match this entry
		}
		m.researchHistory(from)
		return true
	}
	m.endHistorySearch(true)
	return false
}

// searchLine is the caption shown under the box while searching.
func (m *Model) searchLine() string {
	label := "reverse-i-search"
	if m.search.failed {
		label = "failing " + label
	}
	return askStyle.Render("("+label+") `"+m.search.query+"'") +
		hintStyle.Render("  (ctrl+r older · enter to edit · esc to cancel)")
}
