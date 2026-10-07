package tui

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestSlashAsPrompt(t *testing.T) {
	m := newTestModel()
	m.sess.Skills = []SkillCommand{{Name: "deploy", Render: func(a string) string { return a }}}

	cases := []struct {
		in       string
		isPrompt bool
		sent     string // the prompt, when isPrompt
	}{
		// The reported case: a path, followed by a sentence.
		{"/etc/nginx/nginx.conf fails to parse", true, "/etc/nginx/nginx.conf fails to parse"},
		// A lone path is a prompt too: the second "/" gives it away.
		{"/etc/hosts", true, "/etc/hosts"},
		// An unknown first word followed by more text.
		{"/tmp is full", true, "/tmp is full"},
		{"/usr/bin\nwhy is this on PATH", true, "/usr/bin\nwhy is this on PATH"},
		// The escape sends the line with one slash removed.
		{"//help me read this", true, "/help me read this"},
		{"//model", true, "/model"},
		// Commands, aliases and skills stay commands, arguments or not.
		{"/help", false, ""},
		{"/model claude-fable-5", false, ""},
		{"/?", false, ""},
		{"/exit", false, ""},
		{"/allow Bash(go test:*)", false, ""},
		{"/deploy to staging", false, ""},
		// A lone unknown /word is still a command, so it can be reported.
		{"/modle", false, ""},
		{"/", false, ""},
		// Not a slash line at all.
		{"fix the build", false, ""},
	}
	for _, c := range cases {
		got, ok := m.slashAsPrompt(inputText{Display: c.in, Prompt: c.in})
		if ok != c.isPrompt {
			t.Errorf("%q: isPrompt = %v, want %v", c.in, ok, c.isPrompt)
			continue
		}
		if ok && got.Prompt != c.sent {
			t.Errorf("%q: sends %q, want %q", c.in, got.Prompt, c.sent)
		}
	}
}

func TestUnknownSlashSuggestsNearest(t *testing.T) {
	m := newTestModel()
	m.sess.Skills = []SkillCommand{{Name: "deploy"}}
	if got := m.unknownSlashMessage("/modle"); !strings.Contains(got, "Did you mean /mode or /model?") {
		t.Errorf("got %q, want a /model suggestion", got)
	}
	if got := m.unknownSlashMessage("/deplyo"); !strings.Contains(got, "Did you mean /deploy?") {
		t.Errorf("got %q, want the skill suggested", got)
	}
	got := m.unknownSlashMessage("/zzzzzzzz")
	if strings.Contains(got, "Did you mean") {
		t.Errorf("nothing is close to /zzzzzzzz, got %q", got)
	}
	if !strings.Contains(got, "Unknown command /zzzzzzzz.") || !strings.Contains(got, "//") {
		t.Errorf("got %q, want the unknown-command error naming the // escape", got)
	}
}

func TestEditDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"abc", "", 3}, {"/modle", "/model", 1}, {"/modle", "/mode", 1}, {"ab", "ba", 1}, {"/mode", "/model", 1}, {"kitten", "sitting", 3},
	} {
		if got := editDistance(c.a, c.b); got != c.want {
			t.Errorf("editDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// An idle Enter on a line that starts with a path starts a turn with it,
// instead of answering "Unknown command".
func TestIdleSubmitSendsPathAsPrompt(t *testing.T) {
	for _, c := range []struct{ typed, sent string }{
		{"/etc/nginx/nginx.conf fails to parse", "/etc/nginx/nginx.conf fails to parse"},
		{"//model is a word I want explained", "/model is a word I want explained"},
	} {
		m := newPasteModel(t)
		sent := make(chan string, 1)
		m.ctx = context.Background()
		m.events = make(chan tea.Msg, 8)
		m.run = func(_ context.Context, prompt string, _ []anthropic.BetaMessageParam,
			_ agent.Approver, _ tools.Asker, _ tools.Planner, _ agent.Emitter, _ func() agent.Interjection, _ func(string, []string)) (agent.Result, error) {
			sent <- prompt
			return agent.Result{}, nil
		}
		m.input.SetValue(c.typed)
		model, _ := m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
		m = model.(*Model)

		select {
		case got := <-sent:
			if !strings.Contains(got, c.sent) {
				t.Errorf("%q: the model received %q, want it to contain %q", c.typed, got, c.sent)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q: turn never started; transcript:\n%s", c.typed, m.transcript.String())
		}
		if strings.Contains(m.transcript.String(), "Unknown command") {
			t.Errorf("%q: should not be reported as an unknown command", c.typed)
		}
		// History keeps what was typed, so ↑ Enter routes the same way.
		if last := m.inputHistory[len(m.inputHistory)-1]; last != c.typed {
			t.Errorf("%q: history = %q, want what was typed", c.typed, last)
		}
	}
}

func TestIdleSubmitLoneUnknownSlashStillErrors(t *testing.T) {
	m := newPasteModel(t)
	m.input.SetValue("/modle")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.state != stateIdle {
		t.Errorf("a lone unknown /word should not start a turn, state = %v", m.state)
	}
	if out := m.transcript.String(); !strings.Contains(out, "Unknown command /modle") || !strings.Contains(out, "/model") {
		t.Errorf("want the unknown-command error with a suggestion, got:\n%s", out)
	}
}

// While a turn runs, a path line is queued as a follow-up, not run as a command.
func TestRunningSubmitQueuesPathPrompt(t *testing.T) {
	m := newPasteModel(t)
	m.state = stateRunning
	m.input.SetValue("/var/log/syslog shows the crash")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := peekSteer(m); got != "/var/log/syslog shows the crash" {
		t.Errorf("queued %q, want the path line", got)
	}
	if strings.Contains(m.transcript.String(), "Unknown command") {
		t.Error("a queued path line should not be reported as an unknown command")
	}

	m = newPasteModel(t)
	m.state = stateRunning
	m.input.SetValue("//help is what I need")
	m.onKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := peekSteerPrompt(m); got != "/help is what I need" {
		t.Errorf("the agent would receive %q, want one slash removed", got)
	}
}

// Every name handleSlash's switch accepts must count as a command, or a line
// like "/allow Bash(x)" would be sent to the model as a prompt. This reads the
// switch out of the source so a new case cannot be added without being covered.
func TestEverySlashCaseIsACommand(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "tui.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "handleSlash" {
			return true
		}
		for _, stmt := range fn.Body.List {
			sw, ok := stmt.(*ast.SwitchStmt)
			if !ok {
				continue
			}
			for _, cc := range sw.Body.List {
				for _, e := range cc.(*ast.CaseClause).List {
					if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						s, _ := strconv.Unquote(lit.Value)
						names = append(names, s)
					}
				}
			}
		}
		return false
	})
	if len(names) < 10 {
		t.Fatalf("found only %d case labels in handleSlash; did the switch move?", len(names))
	}
	m := newTestModel()
	for _, n := range names {
		if !m.isSlashCommand(n) {
			t.Errorf("handleSlash accepts %s but isSlashCommand does not; add it to commandList or slashAliases", n)
		}
	}
}
