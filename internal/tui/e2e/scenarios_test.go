//go:build darwin || linux

package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	// doneLine is the end-of-turn summary ("✓ done in 1.2s · …").
	doneLine = regexp.MustCompile(`done in \d`)
	// failedTool marks a tool call that returned an error.
	failedTool = regexp.MustCompile(`✗\s+Bash\b`)
)

// toolLine matches a line that reports a call to the named tool, whether as
// the call header (⚙) or its result (✓/✗).
func toolLine(name string) *regexp.Regexp {
	return regexp.MustCompile(`^\s*[⚙✓✗]\s+` + regexp.QuoteMeta(name) + `\b`)
}

func TestStartup(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	snap := tm.WaitFor(t, "the status line naming the model", 10*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.StatusLine(), mockModel)
	})
	if !snap.InputBox() {
		t.Fatalf("no input box\n%s", snap)
	}
	if strings.Contains(snap.All(), "rgb:") {
		t.Fatalf("a terminal query reply leaked onto the screen\n%s", snap)
	}
}

func TestPlainPromptStreams(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "hello there")
	snap := tm.WaitFor(t, "the reply and the done line", 20*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "Hello from the mock model.") && s.HasLine(doneLine)
	})
	if got := env.Mock.Prompts(); len(got) == 0 || !strings.Contains(got[0], "hello there") {
		t.Fatalf("mock was asked %q, want the typed prompt", got)
	}
	if !snap.InputBox() {
		t.Fatalf("input box gone after the turn\n%s", snap)
	}
}

func TestMultiToolTurn(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "use the tools")
	snap := tm.WaitFor(t, "the final reply", 30*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "Changed the greeting") && s.HasLine(doneLine)
	})
	for _, name := range []string{"TodoWrite", "Read", "Glob", "Bash", "Edit"} {
		if !snap.HasLine(toolLine(name)) {
			t.Errorf("no line for the %s call\n%s", name, snap.All())
		}
	}
	if snap.HasLine(failedTool) {
		t.Errorf("a tool failed\n%s", snap.All())
	}
	if want := 5 + 1; env.Mock.Requests() != want { // five tool steps and the answer
		t.Errorf("mock received %d requests, want %d", env.Mock.Requests(), want)
	}
	got, err := os.ReadFile(filepath.Join(env.Project, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"hello, e2e"`) {
		t.Fatalf("Edit did not change main.go:\n%s", got)
	}
}

func TestFailingBashShowsCross(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "make it fail")
	tm.WaitFor(t, "✗ on the failed Bash call and the end of the turn", 20*time.Second, func(s Snapshot) bool {
		return s.HasLine(failedTool) && strings.Contains(s.All(), "That failed, as expected.")
	})
}

func TestEscInterruptKeepsPartialText(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "go slow")
	tm.WaitFor(t, "the reply to start streaming", 15*time.Second, func(s Snapshot) bool {
		return strings.Count(s.All(), slowSentence) >= 2
	})
	tm.Press(keyEsc)
	deadline := time.Now().Add(10 * time.Second)
	for env.Mock.Aborted() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the stream was not cancelled after Esc\n%s", tm.Snapshot())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The turn is over when a new prompt is accepted and answered.
	env.Prompt(t, tm, "hello again")
	snap := tm.WaitText(t, "Hello from the mock model.", 20*time.Second)
	n := strings.Count(snap.All(), slowSentence)
	if n == 0 {
		t.Fatalf("the partial reply was not kept\n%s", snap)
	}
	if n >= 40 {
		t.Fatalf("the whole reply arrived (%d sentences); Esc did not interrupt\n%s", n, snap)
	}
}

func TestCtrlCTwiceQuits(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	tm.Press(keyCtrlC)
	if exited, err := tm.WaitExit(500 * time.Millisecond); exited {
		t.Fatalf("one Ctrl+C quit (err %v); it should only arm\n%s", err, tm.Snapshot())
	}
	tm.Press(keyCtrlC)
	exited, err := tm.WaitExit(10 * time.Second)
	if !exited {
		t.Fatalf("second Ctrl+C did not quit\n%s", tm.Snapshot())
	}
	if err != nil {
		t.Fatalf("exit after Ctrl+C twice: %v\n%s", err, tm.Snapshot())
	}
}

func TestOSC8LinksExistingFile(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	env.Prompt(t, tm, "show links")
	snap := tm.WaitFor(t, "the reply with links", 20*time.Second, func(s Snapshot) bool {
		return strings.Contains(s.All(), "missing.go") && s.HasLine(doneLine)
	})
	var file, web bool
	for _, l := range snap.Links {
		switch {
		case strings.HasPrefix(l, "file://") && strings.Contains(l, filepath.Join(env.Project, "main.go")):
			file = true
		case strings.HasPrefix(l, "https://go.dev/doc"):
			web = true
		case strings.Contains(l, "missing.go"):
			t.Errorf("linked a file that does not exist: %s", l)
		}
	}
	if !file || !web {
		t.Fatalf("links = %q; want one to %s and one to https://go.dev/doc/\n%s",
			snap.Links, filepath.Join(env.Project, "main.go"), snap)
	}
}

// Shift+Enter inserts a newline on terminals that can tell it from Enter:
// Bubble Tea v2 parses both encodings in use, the Kitty keyboard protocol's
// (CSI 13;2u) and xterm modifyOtherKeys' (CSI 27;2;13~).
func TestShiftEnterInsertsANewline(t *testing.T) {
	env := newEnv(t)
	tm := env.Start(t)
	tm.Type("first")
	tm.Press("\x1b[13;2u")
	tm.Type("second")
	tm.Press("\x1b[27;2;13~")
	tm.Type("hello")
	tm.WaitText(t, "hello", 5*time.Second)
	if n := env.Mock.Requests(); n != 0 {
		t.Fatalf("Shift+Enter sent the prompt (%d requests)\n%s", n, tm.Snapshot())
	}
	tm.Press(keyEnter)
	tm.WaitFor(t, "the reply", 20*time.Second, func(s Snapshot) bool { return s.HasLine(doneLine) })
	if got := env.Mock.Prompts(); len(got) == 0 || !strings.Contains(got[0], "first\nsecond\nhello") {
		t.Fatalf("the model was sent %q, want the three lines", got)
	}
}
