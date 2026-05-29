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
	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/tools"
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

// writeSpec is a test helper: dump `body` to a fresh PRD.md under TempDir and
// return its path. Used by the loopNext gate tests so CountUnchecked has a real
// file to read (it abstains on read errors, which is fine for older tests that
// don't care about the mechanical gate).
func writeSpec(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "PRD.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGoalLoopNext(t *testing.T) {
	// A normal iteration that isn't complete: decrement and continue with the
	// iteration prompt.
	m := newTestModel()
	m.loopTotal, m.loopRemaining, m.loopSpecPath = 3, 3, "PRD.md"
	if next := m.loopNext(agent.Result{Text: "made progress"}, nil); !strings.Contains(next, "iterating toward") {
		t.Fatalf("should continue with the iteration prompt; got %q", next)
	}
	if m.loopRemaining != 2 || !strings.Contains(m.transcript.String(), "↻ iteration 2/3") {
		t.Errorf("rem=%d transcript=%q", m.loopRemaining, m.transcript.String())
	}

	// First <goal-complete/> with all checkboxes ticked: fires the verification
	// turn (doesn't exit yet). loopRemaining and budget stay untouched.
	m = newTestModel()
	m.loopTotal, m.loopRemaining = 5, 5
	m.loopSpecPath = writeSpec(t, "# Goal\n\n- [x] all done\n")
	next := m.loopNext(agent.Result{Text: "all good <goal-complete/>"}, nil)
	if !strings.Contains(next, "final independent review") {
		t.Fatalf("first <goal-complete/> should fire verification; got %q", next)
	}
	if !m.loopVerifying || m.loopRemaining != 5 {
		t.Errorf("verifying=%v rem=%d, want true/5", m.loopVerifying, m.loopRemaining)
	}
	// Verification turn itself emits <goal-complete/>: NOW the loop exits.
	if got := m.loopNext(agent.Result{Text: "<goal-complete/>"}, nil); got != "" {
		t.Fatalf("verified completion must stop the loop; got %q", got)
	}
	if m.loopRemaining != 0 || !strings.Contains(m.transcript.String(), "goal complete (verified) in 1") {
		t.Errorf("rem=%d transcript=%q", m.loopRemaining, m.transcript.String())
	}

	// <goal-complete/> with unchecked items in the spec: claim rejected,
	// iteration continues. Mechanical gate must catch the "model forgot" case
	// even before verification gets a chance.
	m = newTestModel()
	m.loopTotal, m.loopRemaining = 5, 5
	m.loopSpecPath = writeSpec(t, "- [x] done\n- [ ] still open\n- [ ] also open\n")
	next = m.loopNext(agent.Result{Text: "<goal-complete/>"}, nil)
	if !strings.Contains(next, "iterating toward") {
		t.Fatalf("rejected completion should fall back to iteration prompt; got %q", next)
	}
	tr := m.transcript.String()
	if !strings.Contains(tr, "completion claim rejected: 2 unchecked") {
		t.Errorf("expected rejection line with count; got %q", tr)
	}
	if m.loopRemaining != 4 || m.loopVerifying {
		t.Errorf("rem=%d verifying=%v, want 4/false", m.loopRemaining, m.loopVerifying)
	}

	// Verification turn says "more work needed": resume iteration, no exit.
	m = newTestModel()
	m.loopTotal, m.loopRemaining = 5, 4
	m.loopSpecPath = writeSpec(t, "- [x] all done\n")
	m.loopVerifying = true // pretend we just sent the verification prompt
	next = m.loopNext(agent.Result{Text: "actually Phase 7 still needs auth wiring"}, nil)
	if !strings.Contains(next, "iterating toward") {
		t.Fatalf("verification flagging work should resume iteration; got %q", next)
	}
	if m.loopVerifying || m.loopRemaining != 3 {
		t.Errorf("verifying=%v rem=%d, want false/3", m.loopVerifying, m.loopRemaining)
	}
	if !strings.Contains(m.transcript.String(), "verification flagged remaining work") {
		t.Errorf("expected verification-flag line; got %q", m.transcript.String())
	}

	// Cap reached without completing: run one wrap-up turn, then stop.
	m = newTestModel()
	m.loopTotal, m.loopRemaining, m.loopSpecPath = 2, 1, "PRD.md"
	next = m.loopNext(agent.Result{Text: "still going"}, nil)
	if !strings.Contains(next, "stopping before the goal is complete") {
		t.Fatalf("cap should trigger a wrap-up turn; got %q", next)
	}
	if !m.loopWrapUp || !strings.Contains(m.transcript.String(), "summarising progress") {
		t.Errorf("expected wrap-up state; wrapUp=%v transcript=%q", m.loopWrapUp, m.transcript.String())
	}
	// The turn after the wrap-up stops the loop and points at the spec.
	if after := m.loopNext(agent.Result{Text: "summary written"}, nil); after != "" {
		t.Fatalf("loop should stop after the wrap-up turn; got %q", after)
	}
	if m.loopWrapUp || !strings.Contains(m.transcript.String(), "summary written to PRD.md") {
		t.Errorf("wrapUp=%v transcript=%q", m.loopWrapUp, m.transcript.String())
	}

	// An error (e.g. Esc/cancel) halts the loop with no wrap-up.
	m = newTestModel()
	m.loopTotal, m.loopRemaining = 4, 4
	if next := m.loopNext(agent.Result{}, context.Canceled); next != "" {
		t.Fatalf("an errored turn must halt the loop; got %q", next)
	}
	if m.loopRemaining != 0 || m.loopWrapUp || !strings.Contains(m.transcript.String(), "halted") {
		t.Errorf("rem=%d wrapUp=%v transcript=%q", m.loopRemaining, m.loopWrapUp, m.transcript.String())
	}
}

func TestGoalLoopMergeHintOnStop(t *testing.T) {
	m := newTestModel()
	m.loopTotal, m.loopRemaining = 2, 2
	m.loopSpecPath = writeSpec(t, "- [x] done\n")
	m.loopBranch, m.loopBaseBranch = "klaudia/goal-x", "main"
	// First <goal-complete/> kicks off the verification turn — merge hint not
	// emitted yet.
	m.loopNext(agent.Result{Text: "done <goal-complete/>"}, nil)
	if strings.Contains(m.transcript.String(), "git merge") {
		t.Fatalf("merge hint should wait until verification confirms; got %q", m.transcript.String())
	}
	// Verification turn also says complete — NOW the loop stops and points
	// the user at the merge command.
	m.loopNext(agent.Result{Text: "<goal-complete/>"}, nil)
	if !strings.Contains(m.transcript.String(), "git merge klaudia/goal-x") {
		t.Errorf("expected merge hint after verified completion; got %q", m.transcript.String())
	}
}

func TestLiveUsageEventsUpdateStats(t *testing.T) {
	// One inner-LLM-call usage event should immediately move the session
	// counters — that's the whole point: long /goal iterations show progress
	// in the status bar between inner calls instead of staying at 0 until
	// the whole iteration concludes.
	m := newTestModel()
	m.renderEvent(agent.Event{Type: "usage", InputDelta: 1200, OutputDelta: 300, TurnDelta: 1})
	m.renderEvent(agent.Event{Type: "usage", InputDelta: 800, OutputDelta: 150, TurnDelta: 1})
	if m.statTurns != 2 || m.statIn != 2000 || m.statOut != 450 {
		t.Errorf("session counters after live events: turns=%d in=%d out=%d, want 2/2000/450",
			m.statTurns, m.statIn, m.statOut)
	}
	if m.turnLiveTurns != 2 || m.turnLiveIn != 2000 || m.turnLiveOut != 450 {
		t.Errorf("per-turn tally: turns=%d in=%d out=%d, want 2/2000/450",
			m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut)
	}
}

func TestReconcileUsageNoDoubleCount(t *testing.T) {
	// Three scenarios for the doneMsg reconciliation arithmetic:
	tests := []struct {
		name        string
		liveTurns   int
		liveIn      int64
		liveOut     int64
		resTurns    int
		resIn       int64
		resOut      int64
		wantTurns   int
		wantIn      int64
		wantOut     int64
		description string
	}{
		{
			name:      "all live events arrived → reconcile adds zero",
			liveTurns: 3, liveIn: 2000, liveOut: 450,
			resTurns: 3, resIn: 2000, resOut: 450,
			wantTurns: 3, wantIn: 2000, wantOut: 450,
			description: "no double-count when live tally matches Result",
		},
		{
			name:      "some live events dropped → reconcile catches up",
			liveTurns: 2, liveIn: 1500, liveOut: 300,
			resTurns: 3, resIn: 2000, resOut: 450,
			wantTurns: 3, wantIn: 2000, wantOut: 450,
			description: "channel pressure shouldn't show stale totals",
		},
		{
			name:      "no live events (back-compat path) → straight accumulation",
			liveTurns: 0, liveIn: 0, liveOut: 0,
			resTurns: 3, resIn: 2000, resOut: 450,
			wantTurns: 3, wantIn: 2000, wantOut: 450,
			description: "back-compat for callers without usage events",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel()
			m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut = tc.liveTurns, tc.liveIn, tc.liveOut
			m.statTurns, m.statIn, m.statOut = tc.liveTurns, tc.liveIn, tc.liveOut
			// Inline the reconciliation arithmetic from the doneMsg handler.
			m.statTurns += tc.resTurns - m.turnLiveTurns
			m.statIn += tc.resIn - m.turnLiveIn
			m.statOut += tc.resOut - m.turnLiveOut
			m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut = 0, 0, 0
			if m.statTurns != tc.wantTurns || m.statIn != tc.wantIn || m.statOut != tc.wantOut {
				t.Errorf("%s: got turns=%d in=%d out=%d, want %d/%d/%d",
					tc.description, m.statTurns, m.statIn, m.statOut,
					tc.wantTurns, tc.wantIn, tc.wantOut)
			}
			if m.turnLiveTurns != 0 || m.turnLiveIn != 0 || m.turnLiveOut != 0 {
				t.Errorf("per-turn tally must be cleared after reconcile; got %d/%d/%d",
					m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut)
			}
		})
	}
}

func TestLoopNextHaltsOnStalledTurn(t *testing.T) {
	// Anthropic streaming can complete a request cleanly but with no content
	// during session-limit/quota throttling (no 429, no err — just zero
	// tokens and zero turns). Without this check the loop ate its iteration
	// budget on empty turns while the spinner span. Halt with a clear line so
	// the user can see the cause rather than chase a phantom.
	m := newTestModel()
	m.loopTotal, m.loopRemaining = 5, 5
	m.loopSpecPath = writeSpec(t, "- [ ] still open\n")
	next := m.loopNext(agent.Result{}, nil) // no err, no tokens, no turns
	if next != "" {
		t.Fatalf("stalled turn must halt the loop; got %q", next)
	}
	if m.loopRemaining != 0 {
		t.Errorf("loopRemaining should be 0 after stall; got %d", m.loopRemaining)
	}
	if !strings.Contains(m.transcript.String(), "empty response from model") {
		t.Errorf("expected stall message in scrollback; got %q", m.transcript.String())
	}
}

func TestLoopIsStallShape(t *testing.T) {
	// Belt-and-braces unit on the predicate. Only a no-error / no-turns /
	// no-tokens result counts — a legitimate finish with tokens is fine, and
	// an error is handled by the regular halt path.
	cases := []struct {
		name string
		res  agent.Result
		err  error
		want bool
	}{
		{"empty + nil err", agent.Result{}, nil, true},
		{"has text", agent.Result{Text: "ok"}, nil, false},
		{"has output", agent.Result{OutputTokens: 12}, nil, false},
		{"has turn", agent.Result{NumTurns: 1}, nil, false},
		{"has err", agent.Result{}, context.Canceled, false},
	}
	for _, tc := range cases {
		if got := loopIsStall(tc.res, tc.err); got != tc.want {
			t.Errorf("%s: loopIsStall = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFormatStats(t *testing.T) {
	// Unknown limit (0) → no context suffix; users get the bare counters they
	// had before so /stats still works on unmapped models.
	t.Run("unknown limit omits suffix", func(t *testing.T) {
		got := formatStats(3, 1500, 800, 0, 0, "")
		if !strings.Contains(got, "turns=3") || !strings.Contains(got, "input_tokens=1500") {
			t.Errorf("base counters missing: %q", got)
		}
		if strings.Contains(got, "context:") {
			t.Errorf("context suffix should be omitted when limit=0: %q", got)
		}
	})
	// Known limit → suffix shows resident vs limit, percentage, and source so
	// users can tell at a glance whether they're close to autocompaction.
	t.Run("known limit shows resident and source", func(t *testing.T) {
		got := formatStats(5, 12345, 2000, 60000, 200000, "model default")
		if !strings.Contains(got, "context: ~60000/200000") {
			t.Errorf("expected resident/limit ratio: %q", got)
		}
		if !strings.Contains(got, "30%") {
			t.Errorf("expected 30%% usage: %q", got)
		}
		if !strings.Contains(got, "model default") {
			t.Errorf("expected source label: %q", got)
		}
	})
}

func TestPrepareFirstLoopTurnStubFix(t *testing.T) {
	// Spec body mentions Phases 0–4 but the Progress tracker only stubs out
	// 0–1 — exactly the huedoku failure mode. The first turn must be a
	// stub-fix turn (not the regular iteration prompt) and the warning line
	// must show which phases are missing.
	m := newTestModel()
	specPath := writeSpec(t, "# Goal\n\n## Phase 0 — Setup\n\n## Phase 1 — Build\n\n## Phase 4 — Validation\n\n## Progress\n- [x] Phase 0 — Setup\n- [ ] Phase 1 — Build\n")
	prompt := m.prepareFirstLoopTurn(specPath, 10)
	if !m.loopStubFixing {
		t.Errorf("loopStubFixing should be set when body phases outstrip the tracker")
	}
	if !strings.Contains(prompt, "Progress tracker") || !strings.Contains(prompt, "Phase 4 — Validation") {
		t.Errorf("first prompt should be StubFixPrompt listing the missing phase; got %q", prompt)
	}
	tr := m.transcript.String()
	if !strings.Contains(tr, "Progress tracker missing stubs") || !strings.Contains(tr, "Phase 4 — Validation") {
		t.Errorf("expected stub-fix warning listing the missing phase; got %q", tr)
	}
	if !strings.Contains(tr, "stub fix") {
		t.Errorf("iteration banner should label the stub fix; got %q", tr)
	}
}

func TestPrepareFirstLoopTurnCleanSpec(t *testing.T) {
	// Spec without a Progress tracker (or with one that covers every body
	// phase) goes straight into the normal iteration prompt — no stub-fix
	// detour, no warning line.
	m := newTestModel()
	specPath := writeSpec(t, "# Goal\n\n## Objective\nShip a thing.\n\n## Acceptance criteria\n- [ ] tests pass\n")
	prompt := m.prepareFirstLoopTurn(specPath, 10)
	if m.loopStubFixing {
		t.Errorf("loopStubFixing must stay false for a single-phase spec")
	}
	if !strings.Contains(prompt, "iterating toward") {
		t.Errorf("expected the regular iteration prompt; got %q", prompt)
	}
	if strings.Contains(m.transcript.String(), "Progress tracker missing stubs") {
		t.Errorf("no stub-fix warning expected; got %q", m.transcript.String())
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
