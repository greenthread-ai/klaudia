package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/tools"
)

func newTestModel() *Model {
	in := newPromptInput()
	return &Model{input: in, sess: &Session{}, histPos: 0, follow: true}
}

func TestHistoryNavigation(t *testing.T) {
	m := newTestModel()
	m.pushHistory("first")
	m.pushHistory("second")

	// Stash a fresh draft, then walk up to older entries.
	m.input.SetValue("draft")
	m.navigateHistory(true) // → "second"
	if m.input.Value() != "second" {
		t.Fatalf("up once = %q, want second", m.input.Value())
	}
	m.navigateHistory(true) // → "first"
	if m.input.Value() != "first" {
		t.Fatalf("up twice = %q, want first", m.input.Value())
	}
	m.navigateHistory(true) // clamp at oldest
	if m.input.Value() != "first" {
		t.Fatalf("up clamped = %q, want first", m.input.Value())
	}
	m.navigateHistory(false) // → "second"
	m.navigateHistory(false) // → draft restored
	if m.input.Value() != "draft" {
		t.Fatalf("down to draft = %q, want draft", m.input.Value())
	}
}

func TestCtrlJInsertsNewline(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("first")

	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m.input.InsertString("second")

	if got := m.input.Value(); got != "first\nsecond" {
		t.Fatalf("input after Ctrl+J = %q, want %q", got, "first\nsecond")
	}
}

func TestEnterSubmitsMultilinePrompt(t *testing.T) {
	m := newTestModel()
	m.ctx = context.Background()
	m.events = make(chan tea.Msg, 1)
	promptCh := make(chan string, 1)
	m.run = func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, approver agent.Approver, asker tools.Asker, planner tools.Planner, emit agent.Emitter) (agent.Result, error) {
		promptCh <- prompt
		return agent.Result{}, nil
	}
	m.input.SetValue("first\nsecond")

	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyEnter})

	select {
	case got := <-promptCh:
		if got != "first\nsecond" {
			t.Fatalf("submitted prompt = %q, want %q", got, "first\nsecond")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for submitted prompt")
	}
	if m.input.Value() != "" {
		t.Fatalf("input after submit = %q, want empty", m.input.Value())
	}
}

func TestHistoryArrowsInMultilineInputMoveCursor(t *testing.T) {
	m := newTestModel()
	m.pushHistory("history")
	m.input.SetValue("first\nsecond")

	_, _ = m.onKey(tea.KeyMsg{Type: tea.KeyUp})

	if got := m.input.Value(); got != "first\nsecond" {
		t.Fatalf("up in multiline input changed value to %q", got)
	}
}

func TestPushHistoryDedupesConsecutive(t *testing.T) {
	m := newTestModel()
	m.pushHistory("a")
	m.pushHistory("a")
	m.pushHistory("b")
	if len(m.inputHistory) != 2 {
		t.Errorf("history = %v, want [a b]", m.inputHistory)
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := commonPrefix([]string{"internal/api", "internal/agent"}); got != "internal/ag" && got != "internal/a" {
		// longest shared prefix of the two is "internal/a"
		if got != "internal/a" {
			t.Errorf("commonPrefix = %q", got)
		}
	}
	if got := commonPrefix([]string{"abc"}); got != "abc" {
		t.Errorf("single = %q", got)
	}
	if got := commonPrefix([]string{"abc", "xyz"}); got != "" {
		t.Errorf("disjoint = %q, want empty", got)
	}
}

func TestFmtDuration(t *testing.T) {
	cases := map[time.Duration]string{
		500 * time.Millisecond:          "500ms",
		12300 * time.Millisecond:        "12.3s",
		(2*time.Minute + 5*time.Second): "2m05s",
	}
	for d, want := range cases {
		if got := fmtDuration(d); got != want {
			t.Errorf("fmtDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSlashSuggestions(t *testing.T) {
	m := newTestModel()
	m.sess.Skills = []SkillCommand{{Name: "review"}}

	m.input.SetValue("/co")
	got := m.slashSuggestions()
	// /compact, /config, /context, /commit all start with /co
	if len(got) < 3 {
		t.Errorf("/co suggestions = %v, want several", got)
	}
	for _, g := range got {
		if !strings.HasPrefix(g, "/co") {
			t.Errorf("suggestion %q does not match /co", g)
		}
	}

	// A skill is suggested.
	m.input.SetValue("/rev")
	if got := m.slashSuggestions(); len(got) != 1 || got[0] != "/review" {
		t.Errorf("/rev = %v, want [/review]", got)
	}

	// No suggestions once a space is typed (args started).
	m.input.SetValue("/model gpt")
	if got := m.slashSuggestions(); got != nil {
		t.Errorf("with args = %v, want nil", got)
	}
}

func TestHelpAndSuggestionsShareSource(t *testing.T) {
	help := slashHelp()
	// Every canonical command name appears in /help (single source of truth).
	for _, name := range builtinCommands() {
		if !strings.Contains(help, name) {
			t.Errorf("/help is missing %q", name)
		}
	}
	// The removed /permissions alias should be gone everywhere.
	for _, name := range builtinCommands() {
		if name == "/permissions" {
			t.Error("/permissions alias should have been removed")
		}
	}
	if strings.Contains(help, "/permissions") {
		t.Error("/help still references /permissions")
	}
	// /mode is present.
	var hasMode bool
	for _, n := range builtinCommands() {
		if n == "/mode" {
			hasMode = true
		}
	}
	if !hasMode {
		t.Error("/mode missing from commands")
	}
}

func TestCompleteSlashCommonPrefix(t *testing.T) {
	m := newTestModel()
	m.input.SetValue("/co")
	m.completeSlash() // multiple matches → common prefix (at least "/co")
	if !strings.HasPrefix(m.input.Value(), "/co") {
		t.Errorf("completeSlash = %q", m.input.Value())
	}

	m.input.SetValue("/doc")
	m.completeSlash() // unique → "/doctor "
	if m.input.Value() != "/doctor " {
		t.Errorf("unique completeSlash = %q, want '/doctor '", m.input.Value())
	}
}

func TestMatchPaths(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.go", "alfred.txt", "beta.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := matchPaths(dir, "al")
	if len(got) != 2 {
		t.Fatalf("matchPaths(al) = %v, want alpha.go + alfred.txt", got)
	}
	if matchPaths(dir, "zzz") != nil {
		t.Errorf("no-match should be nil")
	}
}

func TestSlashCommandsDuringProcessing(t *testing.T) {
	m := newTestModel()
	m.state = stateRunning

	// A display command runs while the model works and leaves state running.
	m.input.SetValue("/help")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stateRunning {
		t.Fatalf("state = %v after /help, want stateRunning", m.state)
	}
	if !strings.Contains(m.transcript.String(), "/help") {
		t.Error("transcript should echo the /help command run mid-turn")
	}

	// A history-mutating command is refused (not executed) while a turn runs.
	m.history = []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))}
	m.input.SetValue("/clear")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.history) == 0 {
		t.Error("/clear must not wipe history while a turn is in flight")
	}
	if !strings.Contains(m.transcript.String(), "isn't available while Klaudia is working") {
		t.Error("expected the busy-guard hint for /clear")
	}

	// Plain (non-slash) text still queues as a follow-up rather than dispatching.
	m.input.SetValue("do the thing")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.queued != "do the thing" {
		t.Fatalf("queued = %q, want the plain text queued", m.queued)
	}
}

func TestGoalLoopAdvance(t *testing.T) {
	// A normal iteration that isn't complete: decrement and continue.
	m := newTestModel()
	m.loopTotal, m.loopRemaining = 3, 3
	if !m.loopAdvance(agent.Result{Text: "made progress"}, nil) {
		t.Fatal("should continue while iterations remain and goal incomplete")
	}
	if m.loopRemaining != 2 {
		t.Fatalf("loopRemaining = %d, want 2", m.loopRemaining)
	}
	if !strings.Contains(m.transcript.String(), "↻ iteration 2/3") {
		t.Errorf("expected iteration progress line; got %q", m.transcript.String())
	}

	// Completion token stops the loop immediately.
	m = newTestModel()
	m.loopTotal, m.loopRemaining = 5, 5
	if m.loopAdvance(agent.Result{Text: "all good <goal-complete/>"}, nil) {
		t.Fatal("completion must stop the loop")
	}
	if m.loopRemaining != 0 || !strings.Contains(m.transcript.String(), "goal complete in 1") {
		t.Errorf("expected completion at iter 1; rem=%d transcript=%q", m.loopRemaining, m.transcript.String())
	}

	// Last iteration with no completion: stop at the cap.
	m = newTestModel()
	m.loopTotal, m.loopRemaining = 2, 1
	if m.loopAdvance(agent.Result{Text: "still going"}, nil) {
		t.Fatal("must not continue past the iteration cap")
	}
	if m.loopRemaining != 0 || !strings.Contains(m.transcript.String(), "stopped after 2") {
		t.Errorf("expected cap stop; rem=%d transcript=%q", m.loopRemaining, m.transcript.String())
	}

	// An error (e.g. Esc/cancel) halts the loop.
	m = newTestModel()
	m.loopTotal, m.loopRemaining = 4, 4
	if m.loopAdvance(agent.Result{}, context.Canceled) {
		t.Fatal("an errored turn must halt the loop")
	}
	if m.loopRemaining != 0 || !strings.Contains(m.transcript.String(), "halted") {
		t.Errorf("expected halt; rem=%d transcript=%q", m.loopRemaining, m.transcript.String())
	}
}

func TestGoalRunRequiresSpec(t *testing.T) {
	m := newTestModel()
	m.sess.CWD = t.TempDir() // no PRD.md / GOAL.md
	m.startGoalLoop(nil)
	if m.loopRemaining != 0 {
		t.Errorf("loop should not start without a spec; rem=%d", m.loopRemaining)
	}
	if !strings.Contains(m.transcript.String(), "No goal spec found") {
		t.Errorf("expected a missing-spec error; got %q", m.transcript.String())
	}
}

func TestGoalSettingToggle(t *testing.T) {
	m := newTestModel()
	m.sess.CWD = t.TempDir()
	m.toggleGoalSetting()
	if !m.goalSetting {
		t.Fatal("first /goal should enter goal-setting")
	}
	if !strings.Contains(m.transcript.String(), "no spec yet") {
		t.Errorf("expected no-spec prompt; got %q", m.transcript.String())
	}
	m.toggleGoalSetting()
	if m.goalSetting {
		t.Fatal("second /goal should leave goal-setting")
	}
}
