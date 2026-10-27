package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
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
