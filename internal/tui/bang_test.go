package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestIsBang(t *testing.T) {
	for _, line := range []string{"!git diff", "! git diff", "  !ls", "!"} {
		if !isBang(line) {
			t.Errorf("%q should be a direct command", line)
		}
	}
	// The far more important half: ordinary prose must never execute.
	for _, line := range []string{
		"", "git diff", "why is !important failing?",
		"the test asserts x != y", "/help", "run !important last",
	} {
		if isBang(line) {
			t.Errorf("%q would have been executed as a shell command", line)
		}
	}
}

func TestBangCommand(t *testing.T) {
	for in, want := range map[string]string{
		"!git diff":        "git diff",
		"! git diff":       "git diff",
		"  !  ls -la  ":    "ls -la",
		"!":                "",
		"!echo 'hi there'": "echo 'hi there'",
	} {
		if got := bangCommand(in); got != want {
			t.Errorf("bangCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

// §14: interpretation vs execution must be obvious *before* Enter. A `!` at the
// start of a long line is easy to lose track of, and the cost of losing track
// is running something you meant to say.
func TestPromptMarkerFollowsTheLine(t *testing.T) {
	m := newTestModel()
	m.ready = true

	m.input.SetValue("why is the test failing")
	m.syncInputHeight()
	if m.promptIsBang {
		t.Error("prose showed the shell marker")
	}

	m.input.SetValue("!git diff")
	m.syncInputHeight()
	if !m.promptIsBang {
		t.Error("a direct command did not show the shell marker")
	}

	m.input.SetValue("git diff")
	m.syncInputHeight()
	if m.promptIsBang {
		t.Error("the shell marker stuck after the ! was deleted")
	}
}

// The output has to reach the model, or "revert that" has no referent and
// running a command here is worse than opening another terminal.
func TestShellOutputBecomesContextForTheNextTurn(t *testing.T) {
	m := newTestModel()
	m.recordShellContext(bangResultMsg{
		command: "git diff --stat",
		output:  " src/api.ts | 4 ++--\n 1 file changed\n",
		code:    0,
	})
	ctx := m.takeShellContext()
	for _, want := range []string{"git diff --stat", "src/api.ts", "The user ran this"} {
		if !strings.Contains(ctx, want) {
			t.Errorf("context missing %q:\n%s", want, ctx)
		}
	}
	// Drained: the same command must not be prepended to every later turn.
	if again := m.takeShellContext(); again != "" {
		t.Errorf("shell context was not drained: %q", again)
	}
}

func TestShellContextIsClipped(t *testing.T) {
	m := newTestModel()
	m.recordShellContext(bangResultMsg{
		command: "make build",
		output:  strings.Repeat("compiling something\n", 5000),
		code:    0,
	})
	ctx := m.takeShellContext()
	if len(ctx) > 8000 {
		t.Errorf("a build log of %d bytes went into the prompt whole", len(ctx))
	}
	if !strings.Contains(ctx, "more lines") {
		t.Error("truncation was not announced")
	}
}

func TestShellContextAccumulatesInOrder(t *testing.T) {
	m := newTestModel()
	m.recordShellContext(bangResultMsg{command: "git status", output: "clean"})
	m.recordShellContext(bangResultMsg{command: "git log -1", output: "abc123"})
	ctx := m.takeShellContext()
	if strings.Index(ctx, "git status") > strings.Index(ctx, "git log") {
		t.Errorf("commands are out of order:\n%s", ctx)
	}
}

// A failing command has to look like one.
func TestFailingShellCommandIsMarked(t *testing.T) {
	m := newTestModel()
	m.onBangResult(bangResultMsg{command: "false", output: "", code: 1})
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "exit 1") {
		t.Errorf("a non-zero exit was not shown:\n%s", out)
	}
}

// An interactive program typed by hand must not hang the UI.
func TestBangRefusesTTYPrograms(t *testing.T) {
	m := newTestModel()
	m.sess.CWD = t.TempDir()
	m.runBang("!vim notes.txt")
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "Edit tool") {
		t.Errorf("vim was not refused with an alternative:\n%s", out)
	}
}

// Enter on a `!` line runs it instead of sending it to the model.
func TestEnterOnBangLineRuns(t *testing.T) {
	m := newTestModel()
	m.ready = true
	m.sess.CWD = t.TempDir()
	m.setState(stateIdle)
	m.input.SetValue("!echo hello")

	_, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on a ! line produced no command to run")
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want running", m.state)
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		t.Errorf("input was not cleared: %q", m.input.Value())
	}
	out := stripANSI(m.transcript.String())
	if !strings.Contains(out, "$ echo hello") {
		t.Errorf("the command was not echoed:\n%s", out)
	}
}

// A command that changes this machine is not blocked — the user typed it — but
// it is named, so a line pasted from a README says what it does first.
func TestBangNamesTheZoneWithoutBlocking(t *testing.T) {
	m := newTestModel()
	m.sess.CWD = t.TempDir()

	if note := m.bangZoneNote("go test ./..."); note != "" {
		t.Errorf("ordinary project work was annotated: %q", note)
	}
	note := m.bangZoneNote("sudo systemctl restart nginx")
	if note == "" {
		t.Fatal("a host change was not named")
	}
	if !strings.Contains(note, "nginx") {
		t.Errorf("the note does not say what it does: %q", note)
	}
	if !strings.Contains(note, "because you asked") {
		t.Errorf("the note reads as a refusal rather than a heads-up: %q", note)
	}
}
