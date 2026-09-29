package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Candidates for an ambiguous @path belong under the prompt while Tab is
// cycling, not in scrollback where they outlive the moment.
func TestAtCandidatesStayOutOfScrollback(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "alpha.go"))
	mustWrite(t, filepath.Join(dir, "alpine.go"))

	m := newTestModel()
	m.resize(80, 24)
	m.sess.CWD = dir
	m.input.SetValue("@alp")
	m.input.CursorEnd()
	m.completeAtPath()

	if out := visibleText(m.transcript.String()); strings.Contains(out, "candidates") || strings.Contains(out, "alpine.go") {
		t.Errorf("candidates were printed into scrollback:\n%s", out)
	}
	line := visibleText(m.atCandidateLine())
	if !strings.Contains(line, "alpha.go") || !strings.Contains(line, "alpine.go") {
		t.Errorf("candidate line should list both matches, got %q", line)
	}
	if !strings.Contains(visibleText(m.bottomView()), "alpine.go") {
		t.Error("candidate line is not rendered under the prompt")
	}

	// Typing ends the cycle, and the line goes with it.
	m.input.SetValue(m.input.Value() + " and")
	if got := m.atCandidateLine(); got != "" {
		t.Errorf("candidate line outlived the cycle: %q", got)
	}
}

// An @ inside a word (an email address, a decorator) is not a reference.
func TestAtInsideWordIsNotCompleted(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "example.go"))

	m := newTestModel()
	m.resize(80, 24)
	m.sess.CWD = dir
	m.input.SetValue("mail bob@exa")
	m.input.CursorEnd()
	m.completeAtPath()
	if got := m.input.Value(); got != "mail bob@exa" {
		t.Errorf("an email address was completed as a path: %q", got)
	}

	m.input.SetValue("look at @exa")
	m.input.CursorEnd()
	m.completeAtPath()
	if got := m.input.Value(); got != "look at @example.go" {
		t.Errorf("an @ after a space should still complete, got %q", got)
	}
}

func TestAtTokenStart(t *testing.T) {
	for in, want := range map[string]int{
		"@a":          0,
		"x @a":        2,
		"x\n@a":       2,
		"bob@a":       -1,
		"no at here":  -1,
		"a@b then @c": 9,
	} {
		if got := atTokenStart(in); got != want {
			t.Errorf("atTokenStart(%q) = %d, want %d", in, got, want)
		}
	}
}

// A follow-up queued during a turn and sent when it ends is a user message
// like any other, so /outline and /search --mine must see it.
func TestQueuedFollowUpIsIndexed(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.ctx = context.Background()
	m.events = make(chan tea.Msg, 4)
	sent := make(chan string, 1)
	m.run = func(ctx context.Context, prompt string, _ []anthropic.BetaMessageParam, _ agent.Approver, _ tools.Asker, _ tools.Planner, _ agent.Emitter, _ func() agent.Interjection, _ func(string, []string)) (agent.Result, error) {
		sent <- prompt
		return agent.Result{}, nil
	}
	m.state = stateRunning
	m.steer.add("also add tests", "also add tests")
	m.update(doneMsg{res: agent.Result{StopReason: "end_turn", Text: "done"}})

	found := false
	for _, e := range m.nav {
		if e.kind == navUser && e.title == "also add tests" {
			found = true
		}
	}
	if !found {
		t.Errorf("queued follow-up missing from the session index: %+v", m.nav)
	}
	if got := <-sent; !strings.Contains(got, "also add tests") {
		t.Errorf("the queued message was not the next turn's prompt: %q", got)
	}
}
