package tui

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// inputText is what the user typed, in the two forms the rest of the code has
// to keep straight.
//
// Display is the paste-chip form: what is echoed to the transcript, pushed to
// history, and put back in the box by ↑. Prompt is the expanded payload: what
// the model, the shell, or a slash command actually receives.
//
// They travel together because every bug in this area has been a submit path
// reading the raw input and sending it — the chip text arriving where the
// payload belonged. Taking both at once makes the choice explicit at each site
// instead of implicit in which accessor was reached for.
type inputText struct {
	Display string
	Prompt  string
}

// Empty reports whether there is nothing to submit.
func (t inputText) Empty() bool { return t.Display == "" }

// readInput reads the box in both forms, trimmed. It does not consume.
func (m *Model) readInput() inputText {
	return inputText{
		Display: strings.TrimSpace(m.input.Value()),
		Prompt:  strings.TrimSpace(m.promptValue()),
	}
}

func (m *Model) inputHeight() int {
	// The input is shown (and editable) when idle, while the model works (for
	// queueing a follow-up), and when answering a question in your own words;
	// other states show a one-line prompt.
	if !m.editableInput() {
		return 1
	}
	// Count wrapped display rows, not logical lines: a single long line that
	// the textarea soft-wraps must grow the box rather than scroll within one
	// row. (LineCount counts logical lines only.)
	h := wrappedRowCount(m.input.Value(), m.input.Width())
	if h < 1 {
		return 1
	}
	if h > m.input.MaxHeight {
		return m.input.MaxHeight
	}
	return h
}

// syncInputHeight resizes the input to fit its content. Inline rendering means
// there is no viewport to reserve space for — the live region is simply however
// tall bottomView draws, clamped by clampBottom.
func (m *Model) syncInputHeight() {
	if !m.ready {
		return
	}
	m.input.SetHeight(m.inputHeight())
	m.syncPromptMarker()
}

// updateInput is the single choke point for feeding a key to the textarea while
// keeping the box the right size. It grows the textarea to its full height
// BEFORE Update, because bubbles' repositionView runs inside Update at the
// current height and only ever scrolls *toward* the cursor — never back up to
// reclaim slack. If a newly-wrapped line grew the box after Update, the view
// would stay scrolled past the top and the first row would vanish (the reported
// bug). Sizing to MaxHeight first means a line that still fits never scrolls the
// top out; syncInputHeight then shrinks the box for display with YOffset already
// at 0. reconcilePastes runs after the edit so deleting a chip drops its payload.
func (m *Model) updateInput(msg tea.Msg) tea.Cmd {
	m.input.SetHeight(m.input.MaxHeight)
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInputHeight()
	m.reconcilePastes()
	// Rank the paths here rather than in View: the menu has to be recomputed
	// when the reference changes, and View runs on every spinner tick.
	if m.editableInput() {
		m.refreshPathMenu()
	}
	return cmd
}

// reconcilePastes drops stored paste payloads that no longer appear anywhere the
// user can still bring them back — neither the live input nor a recall-able
// history entry (which stores the chip form, tui.go pushHistory). This is what
// gives "delete the chip, delete the attachment", and it lets the chip counter
// fall back to #1 once nothing is outstanding instead of climbing forever.
func (m *Model) reconcilePastes() {
	sources := make([]string, 0, len(m.inputHistory)+1)
	sources = append(sources, m.input.Value())
	sources = append(sources, m.inputHistory...)
	m.pastes.reconcile(sources...)
}

// syncPromptMarker swaps the input's leading marker between "›" and "$" as the
// user types.
//
// §14's first checkbox is "interpretation vs execution is obvious". A `!` at
// the start of the line is easy to lose track of mid-sentence, and the cost of
// losing track is running something you meant to say. The marker changes the
// moment the line does, so what will happen on Enter is visible before Enter.
func (m *Model) syncPromptMarker() {
	bang := m.bangRunsHere() && isBang(m.input.Value())
	if bang == m.promptIsBang {
		return
	}
	m.promptIsBang = bang
	marker := "› "
	if bang {
		marker = "$ "
	}
	m.input.SetPromptFunc(promptGutter, func(line int) string {
		if line == 0 {
			return marker
		}
		return "  "
	})
}

// promptValue is the text to actually send: what the user sees, with any paste
// chips substituted back to their verbatim payloads.
//
// Call readInput rather than this. Submit sites need both forms and get them
// together; reaching for one accessor or the other at each site is how the chip
// form ended up being sent to the model, the shell and the steer box. Input
// sizing, type-ahead and history deliberately keep working on the chip form,
// which is the whole point of chipping.
func (m *Model) promptValue() string {
	return m.pastes.expand(m.input.Value())
}

// completeSlash fills the input with the unique command completion, or the
// common prefix when several match.
func (m *Model) completeSlash() {
	sug := m.slashSuggestions()
	if len(sug) == 0 {
		return
	}
	if len(sug) == 1 {
		m.input.SetValue(sug[0] + " ")
	} else {
		m.input.SetValue(commonPrefix(sug))
	}
	m.input.CursorEnd()
}

// completeAtPath inserts the selected @<path> completion. Matching is fuzzy, so
// "@session.ts" finds "src/auth/session.ts"; that makes a common-prefix
// completion meaningless, so Tab inserts the best hit and each further Tab
// advances to the next one. The dropdown (pathmenu.go) shows what is being
// cycled through — before this it was a one-shot banner, and with nothing on
// screen until Tab was pressed the whole feature was invisible.
func (m *Model) completeAtPath() {
	m.menu.dismissed = "" // Tab reopens a menu Esc closed
	cycling := m.menu.open && m.menu.inserted == m.input.Value()
	m.refreshPathMenu()
	if !m.menu.open || len(m.menu.hits) == 0 {
		return
	}
	if cycling {
		m.menu.idx = (m.menu.idx + 1) % len(m.menu.hits)
	}
	m.insertPathHit()
}

// matchPaths returns working-dir-relative file paths that start with partial
// (case-insensitive), sorted, capped. A blank partial lists top entries.
// matchPaths ranks working-dir-relative paths against partial. Ordering is
// deliberate and was previously thrown away: search.Glob returns files
// mtime-descending, and the old implementation immediately re-sorted them
// alphabetically, so "the file I was just working on" ranked below "a_test.go".
// Files Klaudia has actually read or written outrank everything.
func (m *Model) matchPaths(partial string) []string {
	// A reference that names a place outside the project is answered from the
	// filesystem: the index only covers the working directory, so "@~/Desktop/…"
	// used to match nothing and leave the user to type the path by hand.
	if hits, isFS := matchFilesystemPaths(partial); isFS {
		return hits
	}
	root := m.rootDir()
	files := m.paths.files(root, time.Now())

	// A bare "@" matches everything, so the ranking is pure recency and the
	// scoring pass has nothing to decide. Skipping it matters now that the menu
	// recomputes on the keystroke rather than on Tab: this is a sort of up to
	// pathIndexMaxFiles entries, between the key and the character appearing.
	if partial == "" {
		return m.headPaths(root, files)
	}

	type scored struct {
		path  string
		score int
		order int
	}
	recent := map[string]int{}
	for i, p := range m.recentPaths {
		recent[p] = i
	}

	seen := map[string]bool{}
	var out []scored
	consider := func(rel string, order int) {
		if rel == "" || seen[rel] {
			return
		}
		score, ok := fuzzyScore(partial, rel)
		if !ok {
			return
		}
		seen[rel] = true
		if i, isRecent := recent[rel]; isRecent {
			score += 5000 - 50*i
		}
		out = append(out, scored{rel, score, order})
	}
	for i, p := range m.recentPaths {
		consider(p, i)
	}
	for i, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		consider(rel, len(m.recentPaths)+i)
	}

	// Higher score first; ties broken by the incoming order, which is recency.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].order < out[j].order
	})
	if len(out) == 0 {
		return nil
	}
	if len(out) > 200 {
		out = out[:200]
	}
	paths := make([]string, len(out))
	for i, s := range out {
		paths[i] = s.path
	}
	return paths
}

// commonPrefix returns the longest shared leading substring of the inputs.
func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
			if p == "" {
				return ""
			}
		}
	}
	return p
}

// maxInputHistory caps the remembered prompts (ring buffer).
const maxInputHistory = 200

// pushHistory appends a submitted prompt (dropping an immediate duplicate) and
// resets the navigation cursor to "not navigating".
func (m *Model) pushHistory(prompt string) {
	if n := len(m.inputHistory); n == 0 || m.inputHistory[n-1] != prompt {
		m.inputHistory = append(m.inputHistory, prompt)
		if len(m.inputHistory) > maxInputHistory {
			m.inputHistory = m.inputHistory[len(m.inputHistory)-maxInputHistory:]
		}
	}
	m.histPos = len(m.inputHistory)
	m.histDraft = ""
}

// navigateHistory moves through prior prompts: up = older, down = newer. The
// in-progress line is stashed on first up and restored past the newest entry.
func (m *Model) navigateHistory(up bool) {
	if len(m.inputHistory) == 0 {
		return
	}
	if up {
		if m.histPos == len(m.inputHistory) {
			m.histDraft = m.input.Value() // stash the fresh line
		}
		if m.histPos > 0 {
			m.histPos--
		}
	} else {
		if m.histPos < len(m.inputHistory) {
			m.histPos++
		}
	}
	if m.histPos >= len(m.inputHistory) {
		m.input.SetValue(m.histDraft)
	} else {
		m.input.SetValue(m.inputHistory[m.histPos])
	}
	m.input.CursorEnd()
}
