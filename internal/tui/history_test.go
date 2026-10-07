package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"
)

// memHistory is a PromptHistoryStore held in memory.
type memHistory struct {
	stored   []string
	appended []string
	loadErr  error
}

func (h *memHistory) Load() ([]string, error) { return h.stored, h.loadErr }
func (h *memHistory) Append(s string) error {
	h.appended = append(h.appended, s)
	return nil
}

func userText(s string) anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(s))
}

func TestTypedPromptsUnwrapsFraming(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		userText("plain prompt"),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("reply")}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("t1", "tool output", false)),
		userText("Standing goal for this session: ship it\n\nCurrent instruction: under a goal"),
		userText("The user ran this in the terminal:\n\n```\n$ cat secrets\nSECRET-OUTPUT\n\nmore\n```\n(exit 0)\n\nafter a bang"),
		userText("Files the user has pinned as important for this task. Read them if you have not already, and keep them in mind:\n  a.go\n\npinned prompt"),
		userText("The user interrupted with a new instruction. It applies from now on, including to anything you were about to do:\n\nsteered\n\nThe user asked you to stop after the current step. Do not start anything new."),
		userText("You are autonomously iterating toward the goal defined in prd.md.\nRe-read it."),
		userText("[Conversation compacted to save context. Summary of the prior conversation follows.]\n\nstuff"),
		userText("Errors from the log of job \"web\":\n\n```\nboom\n```\n\nWhat is going wrong?"),
		userText("You are helping the user define a clear goal specification, saved at prd.md.\n\nUser: facilitated"),
	}
	got := typedPrompts(history)
	want := []string{"plain prompt", "under a goal", "after a bang", "pinned prompt", "steered", "facilitated"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("typedPrompts = %q\nwant %q", got, want)
	}
	for _, p := range got {
		if strings.Contains(p, "SECRET-OUTPUT") {
			t.Fatalf("a `!` command's output reached history: %q", p)
		}
	}
}

// A shell block whose end cannot be found is dropped whole, never recalled
// with the output attached.
func TestTypedPromptsDropsUnparsableShellContext(t *testing.T) {
	got := typedPrompts([]anthropic.BetaMessageParam{
		userText("The user ran this in the terminal:\n\n```\n$ env\nTOKEN=abc (truncated oddly"),
	})
	if len(got) != 0 {
		t.Fatalf("typedPrompts = %q, want nothing", got)
	}
}

func TestLoadInputHistoryMergesStoreAndResume(t *testing.T) {
	m := newTestModel()
	m.sess.PromptHistory = &memHistory{stored: []string{"older", "shared"}}
	m.loadInputHistory([]anthropic.BetaMessageParam{userText("shared"), userText("resumed only")})

	want := []string{"older", "shared", "resumed only"}
	if !reflect.DeepEqual(m.inputHistory, want) {
		t.Fatalf("history = %q, want %q", m.inputHistory, want)
	}
	m.navigateHistory(true)
	if got := m.input.Value(); got != "resumed only" {
		t.Fatalf("↑ after resume recalled %q, want the resumed prompt", got)
	}
}

func TestLoadInputHistoryCaps(t *testing.T) {
	m := newTestModel()
	var stored []string
	for i := 0; i < MaxInputHistory; i++ {
		stored = append(stored, strings.Repeat("x", i+1))
	}
	m.sess.PromptHistory = &memHistory{stored: stored}
	m.loadInputHistory([]anthropic.BetaMessageParam{userText("newest")})
	if len(m.inputHistory) != MaxInputHistory || m.inputHistory[len(m.inputHistory)-1] != "newest" {
		t.Fatalf("len = %d, last = %q; want %d ending in newest", len(m.inputHistory), m.inputHistory[len(m.inputHistory)-1], MaxInputHistory)
	}
}

func TestLoadInputHistoryReportsReadFailure(t *testing.T) {
	m := newTestModel()
	m.sess.PromptHistory = &memHistory{loadErr: errors.New("permission denied")}
	m.loadInputHistory(nil)
	if !m.historyFaulted {
		t.Fatal("a failed history read was not reported")
	}
}

func TestPushHistoryStoresTypedLineOnly(t *testing.T) {
	m := newTestModel()
	store := &memHistory{}
	m.sess.PromptHistory = store

	m.pushHistory("fix the build")
	m.pushHistory("fix the build") // immediate repeat: not stored twice
	m.pushHistory("see [#1 pasted · 40 lines]")
	m.pushHistory(strings.Repeat("y", maxStoredPromptBytes+1))

	// A `!` command: the typed line is stored, its output never is.
	m.input.SetValue("!cat notes.txt")
	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.onBangResult(bangResultMsg{command: "cat notes.txt", output: "SECRET-OUTPUT"})

	want := []string{"fix the build", "!cat notes.txt"}
	if !reflect.DeepEqual(store.appended, want) {
		t.Fatalf("stored %q, want %q", store.appended, want)
	}
	if len(m.inputHistory) != 4 {
		t.Fatalf("in-memory history = %q, want all four prompts", m.inputHistory)
	}
}

func keyOf(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// A recalled multi-line entry used to trap the arrows: history browsing only
// ran on a single-line box, so ↑ moved inside the entry and never got past it.
func TestHistoryBrowsesPastMultilineEntry(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)
	m.pushHistory("oldest")
	m.pushHistory("line one\nline two\nline three")
	m.pushHistory("newest")
	m.input.SetValue("draft")

	up := func() string { _, _ = m.onKey(keyOf(tea.KeyUp)); return m.input.Value() }
	down := func() string { _, _ = m.onKey(keyOf(tea.KeyDown)); return m.input.Value() }

	if got := up(); got != "newest" {
		t.Fatalf("↑1 = %q", got)
	}
	if got := up(); got != "line one\nline two\nline three" {
		t.Fatalf("↑2 = %q", got)
	}
	if got := up(); got != "oldest" {
		t.Fatalf("↑3 = %q, want to browse past the multi-line entry", got)
	}
	if got := down(); got != "line one\nline two\nline three" {
		t.Fatalf("↓1 = %q", got)
	}
	if got := down(); got != "newest" {
		t.Fatalf("↓2 = %q, want to browse past the multi-line entry", got)
	}
	if got := down(); got != "draft" {
		t.Fatalf("↓3 = %q, want the draft back", got)
	}
}

// Off the first/last row the arrows still move the cursor within the entry.
func TestHistoryArrowsMoveWithinMiddleOfEntry(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)
	m.pushHistory("other")
	m.input.SetValue("a\nb\nc") // cursor on the last line
	_, _ = m.onKey(keyOf(tea.KeyUp))
	_, _ = m.onKey(keyOf(tea.KeyUp))
	if m.input.Value() != "a\nb\nc" || m.input.Line() != 0 {
		t.Fatalf("value %q line %d: ↑ should walk up the lines first", m.input.Value(), m.input.Line())
	}
	_, _ = m.onKey(keyOf(tea.KeyUp))
	if m.input.Value() != "other" {
		t.Fatalf("↑ on the first line = %q, want history", m.input.Value())
	}
}

func TestCtrlRSearch(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)
	for _, p := range []string{"git status", "make test", "deploy", "go test ./..."} {
		m.pushHistory(p)
	}
	m.input.SetValue("draft")

	_, _ = m.onKey(keyOf(tea.KeyCtrlR))
	if !m.search.active {
		t.Fatal("Ctrl+R did not open the search")
	}
	typeRunes(m, "test")
	if got := m.input.Value(); got != "go test ./..." {
		t.Fatalf("search 'test' = %q, want the newest match", got)
	}
	if !strings.Contains(m.bottomView(), "reverse-i-search") {
		t.Fatalf("bottom view does not show the search:\n%s", m.bottomView())
	}
	_, _ = m.onKey(keyOf(tea.KeyCtrlR))
	if got := m.input.Value(); got != "make test" {
		t.Fatalf("Ctrl+R again = %q, want the next older match", got)
	}
	_, _ = m.onKey(keyOf(tea.KeyCtrlR))
	if got := m.input.Value(); got != "make test" || !m.search.failed {
		t.Fatalf("past the oldest match: value %q failed %v", got, m.search.failed)
	}
	if !strings.Contains(m.bottomView(), "failing reverse-i-search") {
		t.Fatal("a failed search is not shown as failing")
	}

	_, _ = m.onKey(keyOf(tea.KeyEnter))
	if m.search.active || m.state != stateIdle || m.input.Value() != "make test" {
		t.Fatalf("Enter: active %v state %v value %q; want the match left in the box to edit", m.search.active, m.state, m.input.Value())
	}
	// Browsing continues from the accepted entry.
	_, _ = m.onKey(keyOf(tea.KeyDown))
	if got := m.input.Value(); got != "deploy" {
		t.Fatalf("↓ after accepting = %q, want the entry after the match", got)
	}
}

func TestCtrlRCancelRestoresDraft(t *testing.T) {
	for _, cancel := range []tea.KeyType{tea.KeyEsc, tea.KeyCtrlG, tea.KeyCtrlC} {
		m := newTestModel()
		m.resize(100, 40)
		m.pushHistory("make test")
		m.input.SetValue("half-typed")
		_, _ = m.onKey(keyOf(tea.KeyCtrlR))
		typeRunes(m, "make")
		_, _ = m.onKey(keyOf(cancel))
		if m.search.active || m.input.Value() != "half-typed" || m.quitArmed {
			t.Fatalf("%v: active %v value %q quitArmed %v; want the draft back and nothing else", cancel, m.search.active, m.input.Value(), m.quitArmed)
		}
	}
}

func TestCtrlRBackspaceAndPassThrough(t *testing.T) {
	m := newTestModel()
	m.resize(100, 40)
	m.pushHistory("alpha")
	m.pushHistory("beta")
	_, _ = m.onKey(keyOf(tea.KeyCtrlR))
	typeRunes(m, "alx")
	if !m.search.failed || m.input.Value() != "alpha" {
		t.Fatalf("'alx': failed %v value %q", m.search.failed, m.input.Value())
	}
	_, _ = m.onKey(keyOf(tea.KeyBackspace))
	if m.search.failed || m.search.query != "al" {
		t.Fatalf("after backspace: failed %v query %q", m.search.failed, m.search.query)
	}
	// A key the search does not use accepts the match and then acts on it.
	_, _ = m.onKey(keyOf(tea.KeyLeft))
	if m.search.active || m.input.Value() != "alpha" {
		t.Fatalf("← : active %v value %q", m.search.active, m.input.Value())
	}
	typeRunes(m, "!")
	if got := m.input.Value(); got != "alph!a" {
		t.Fatalf("typing after ← = %q, want the cursor moved within the accepted entry", got)
	}
}
