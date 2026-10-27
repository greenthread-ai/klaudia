package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
)

// Rewinding to the first of two prompts restores what both turns changed —
// each file to before the earliest turn that touched it, a file Klaudia
// created deleted — cuts the conversation back, records the cut, and returns
// the prompt to the input box.
func TestRewindRestoresFilesAndConversation(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := gitOutput(dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(name, body string) { os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644) }
	read := func(name string) string { b, _ := os.ReadFile(filepath.Join(dir, name)); return string(b) }
	run("init", "-q")
	write("a.go", "original\n")
	run("add", ".")
	run("-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "-m", "init")

	m := newTestModel()
	m.sess.CWD = dir
	m.base = newBaseline()
	m.touched = map[string]bool{}
	var kept = -1
	m.sess.OnRewind = func(n int) { kept = n }
	user := func(text string) anthropic.BetaMessageParam {
		return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(text))
	}

	// Turn 1: edits a.go.
	m.turnSeq++
	m.noteRewindPoint("make it better")
	m.snapshotBefore("make it better", []string{"a.go"})
	write("a.go", "turn one\n")
	m.touched["a.go"] = true
	m.history = append(m.history, user("make it better"), user("(reply)"))

	// Turn 2: edits a.go again and creates b.go.
	m.turnSeq++
	m.noteRewindPoint("and add b")
	m.snapshotBefore("and add b", []string{"a.go", "b.go"})
	write("a.go", "turn two\n")
	write("b.go", "new\n")
	m.touched["b.go"] = true
	m.history = append(m.history, user("and add b"), user("(reply)"))

	p := m.rewindPoints[0]
	plan := m.planRewind(p.turn)
	if len(plan.Restore) != 1 || len(plan.Delete) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	m.applyRewind(p, plan)

	if got := read("a.go"); got != "original\n" {
		t.Errorf("a.go = %q, want its state before the first turn", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.go")); err == nil {
		t.Error("b.go, created after the point, is still there")
	}
	if len(m.history) != 0 || kept != 0 {
		t.Errorf("history %d messages, recorded keep %d; want 0 and 0", len(m.history), kept)
	}
	if m.input.Value() != "make it better" {
		t.Errorf("input = %q", m.input.Value())
	}
	if len(m.rewindPoints) != 0 || m.checkpoints.len() != 0 {
		t.Errorf("points %d, checkpoints %d left; want none", len(m.rewindPoints), m.checkpoints.len())
	}
}

// Turns Klaudia starts on its own are not offered as points to go back to.
func TestRewindListsOnlyTheUsersPrompts(t *testing.T) {
	m := newTestModel()
	m.noteRewindPoint("fix the parser")
	m.noteRewindPoint("<background-job id=\"bash_1\">…</background-job>")
	m.loopRemaining = 2
	m.noteRewindPoint("iteration prompt")
	if len(m.rewindPoints) != 1 || !strings.Contains(m.rewindPoints[0].prompt, "parser") {
		t.Errorf("points = %+v", m.rewindPoints)
	}
}

// threeTurns builds a model with three prompts and replies in its history.
func threeTurns(t *testing.T) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(100, 30)
	for _, p := range []string{"one", "two", "three"} {
		m.turnSeq++
		m.noteRewindPoint(p)
		m.history = append(m.history,
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(p)),
			anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock("re " + p)}})
	}
	return m
}

// Picking a prompt with conversation before it offers both: rewind, or
// summarise what came before. The first prompt has nothing before it, so it
// goes straight to the rewind confirmation.
func TestRewindOffersSummariseWhenThereIsSomethingBefore(t *testing.T) {
	m := threeTurns(t)
	m.sess.SummariseBefore = func(ctx context.Context, head []anthropic.BetaMessageParam, kept int) ([]anthropic.BetaMessageParam, string, error) {
		return nil, "", nil
	}
	m.chooseRewind(m.rewindPoints[2])
	if m.state != stateAwaitingChoice || len(m.choiceItems) != 2 || !strings.Contains(m.choiceItems[1].label, "Summarise") {
		t.Fatalf("state %v, items %+v", m.state, m.choiceItems)
	}
	m.closeChoice()
	m.chooseRewind(m.rewindPoints[0])
	if m.state != stateAwaitingConfirm {
		t.Fatalf("the first prompt should go straight to the rewind confirmation, state %v", m.state)
	}
}

// Summarising up to the third prompt replaces the first two exchanges with
// the summary, keeps the third verbatim, records how much was kept, and moves
// the third prompt's rewind point to its new index.
func TestSummariseUpToHere(t *testing.T) {
	m := threeTurns(t)
	var gotHead, gotKept int
	m.sess.SummariseBefore = func(ctx context.Context, head []anthropic.BetaMessageParam, kept int) ([]anthropic.BetaMessageParam, string, error) {
		gotHead, gotKept = len(head), kept
		return compaction.ReplaceWithSummary("they did one and two"), "they did one and two", nil
	}
	m.events = make(chan tea.Msg, 1)
	p := m.rewindPoints[2]
	m.summariseBefore(p)
	msg := (<-m.events).(summariseDoneMsg)
	m.onSummariseDone(msg)

	if gotHead != 4 || gotKept != 2 {
		t.Errorf("summarised %d messages keeping %d; want 4 and 2", gotHead, gotKept)
	}
	if len(m.history) != 3 {
		t.Fatalf("history has %d messages, want summary + the third exchange", len(m.history))
	}
	if txt := m.history[1].Content[0].OfText.Text; txt != "three" {
		t.Errorf("first kept message = %q, want the third prompt", txt)
	}
	if len(m.rewindPoints) != 1 || m.rewindPoints[0].hist != 1 {
		t.Errorf("points = %+v, want only the third prompt, at index 1", m.rewindPoints)
	}
	// And the moved point still rewinds to the right place.
	m.applyRewind(m.rewindPoints[0], undoPlan{})
	if len(m.history) != 1 {
		t.Errorf("rewinding after a summary left %d messages, want the summary alone", len(m.history))
	}
}

// A compaction replaces the history the points index, so they are dropped
// rather than left to cut the new history at meaningless positions, and an
// empty /rewind says why.
func TestCompactionForgetsRewindPoints(t *testing.T) {
	m := threeTurns(t)
	m.update(compactDoneMsg{history: compaction.ReplaceWithSummary("s"), summary: "s"})
	if len(m.rewindPoints) != 0 {
		t.Fatalf("points survived a compaction: %+v", m.rewindPoints)
	}
	m.rewindCommand()
	if !strings.Contains(m.transcript.String(), "compacted") {
		t.Errorf("empty /rewind did not say why:\n%s", m.transcript.String())
	}

	m = threeTurns(t)
	m.renderEvent(agent.Event{Type: agent.EventCompaction, Content: "autocompact: summarized prior conversation"})
	if len(m.rewindPoints) != 0 {
		t.Errorf("points survived an autocompact")
	}
}
