package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// tab types value into the idle prompt and presses Tab through the real key
// path, so the tests cover the dispatch as well as the completers.
func tab(m *Model, value string) string {
	if value != "" {
		m.input.SetValue(value)
		m.input.CursorEnd()
	}
	m.onKey(tea.KeyMsg{Type: tea.KeyTab})
	return m.input.Value()
}

func TestArgCompleteTheme(t *testing.T) {
	m := newTestModel()
	if got := tab(m, "/theme dra"); got != "/theme dracula " {
		t.Errorf("unique theme = %q, want %q", got, "/theme dracula ")
	}
	// Case is ignored when matching, and the candidate's own spelling wins.
	if got := tab(m, "/theme NO"); got != "/theme nord " {
		t.Errorf("case-insensitive theme = %q", got)
	}
}

func TestArgCompleteMode(t *testing.T) {
	m := newTestModel()
	if got := tab(m, "/mode pl"); got != "/mode plan " {
		t.Errorf("mode = %q", got)
	}
	// Legacy modes still parse but are not offered, as in the picker.
	if got := tab(m, "/mode acc"); got != "/mode acc" {
		t.Errorf("legacy mode completed to %q; only the picker's modes should be offered", got)
	}
}

// /model completes from the list the last /model fetched and never fetches on
// Tab — a keystroke that waited on the network would freeze the prompt.
func TestArgCompleteModelUsesCacheOnly(t *testing.T) {
	m := newTestModel()
	fetched := 0
	m.sess.ListModels = func(context.Context) ([]api.ModelInfo, error) {
		fetched++
		return []api.ModelInfo{{ID: "claude-opus-5"}}, nil
	}

	if got := tab(m, "/model cl"); got != "/model cl" {
		t.Errorf("completed %q with no cached list", got)
	}
	if fetched != 0 {
		t.Fatalf("Tab fetched the model list %d time(s)", fetched)
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "/model on its own fetches it") {
		t.Errorf("no hint on how to get a list:\n%s", out)
	}

	// The list arrives the way /model delivers it.
	m.update(modelsMsg{models: []api.ModelInfo{
		{ID: "claude-opus-5"}, {ID: "claude-sonnet-5"}, {ID: "gpt-x"},
	}})
	m.onKey(tea.KeyMsg{Type: tea.KeyEsc}) // leave the picker it opened
	if m.state != stateIdle {
		t.Fatalf("picker still open (state %v)", m.state)
	}
	if got := tab(m, "/model gp"); got != "/model gpt-x " {
		t.Errorf("cached model = %q", got)
	}
	if got := tab(m, "/model cl"); got != "/model claude-" {
		t.Errorf("common prefix = %q, want /model claude-", got)
	}
	if fetched != 0 {
		t.Errorf("Tab fetched the model list %d time(s)", fetched)
	}
}

func TestArgCompleteCyclesWhenNoPrefixToAdd(t *testing.T) {
	m := newTestModel()
	m.knownModels = []api.ModelInfo{{ID: "claude-opus-5"}, {ID: "claude-sonnet-5"}}

	// First Tab extends to the shared prefix and lists the candidates.
	if got := tab(m, "/model c"); got != "/model claude-" {
		t.Fatalf("first Tab = %q", got)
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "candidates: claude-opus-5  claude-sonnet-5") {
		t.Errorf("candidates not listed:\n%s", out)
	}
	// Further Tabs cycle, wrapping round.
	for _, want := range []string{"/model claude-opus-5", "/model claude-sonnet-5", "/model claude-opus-5"} {
		if got := tab(m, ""); got != want {
			t.Errorf("cycle Tab = %q, want %q", got, want)
		}
	}
}

func TestArgCompleteJobs(t *testing.T) {
	m := newTestModel()
	m.sess.Jobs = &fakeJobs{jobs: []tools.JobStatus{
		{ID: "bash_1", Name: "api", Running: true},
		{ID: "bash_2", Name: "web", Running: false},
		{ID: "bash_3", Running: true},
	}}

	if got := tab(m, "/restart w"); got != "/restart web " {
		t.Errorf("/restart = %q (a stopped job is what gets restarted)", got)
	}
	if got := tab(m, "/logs a"); got != "/logs api " {
		t.Errorf("/logs = %q", got)
	}
	if got := tab(m, "/logs -f bash"); got != "/logs -f bash_3 " {
		t.Errorf("/logs after a flag = %q (an unnamed job completes by id)", got)
	}
	if got := tab(m, "/logs --e"); got != "/logs --errors " {
		t.Errorf("/logs flag = %q", got)
	}
	if got := tab(m, "/logs api "); got != "/logs api " {
		t.Errorf("/logs completed a second job: %q", got)
	}
	// Only running jobs can be stopped; "all" joins when there are several.
	if got := tab(m, "/stopjob w"); got != "/stopjob w" {
		t.Errorf("/stopjob offered a stopped job: %q", got)
	}
	if got := tab(m, "/stopjob al"); got != "/stopjob all " {
		t.Errorf("/stopjob all = %q", got)
	}

	// A lone running job fills in on the first Tab, without "all" in the way.
	m.sess.Jobs = &fakeJobs{jobs: []tools.JobStatus{{ID: "bash_1", Name: "api", Running: true}}}
	m.cycle = completeCycle{}
	if got := tab(m, "/stopjob "); got != "/stopjob api " {
		t.Errorf("lone job = %q", got)
	}
}

func TestArgCompleteLast(t *testing.T) {
	m := newTestModel()
	for i := 0; i < 12; i++ {
		m.results.add(toolResult{tool: "Bash", content: "x"})
	}
	if got := tab(m, "/last l"); got != "/last list " {
		t.Errorf("/last list = %q", got)
	}
	if got := tab(m, "/last 12"); got != "/last 12 " {
		t.Errorf("/last 12 = %q", got)
	}
	// "1" matches 1, 10, 11, 12: nothing to extend, so Tab cycles, newest first.
	m.cycle = completeCycle{}
	if got := tab(m, "/last 1"); got != "/last 12" {
		t.Errorf("/last 1 = %q, want the newest match first", got)
	}
}

func TestArgCompleteUnpin(t *testing.T) {
	m := newTestModel()
	m.pinned = []string{"internal/tui/tui.go", "README.md"}
	if got := tab(m, "/unpin RE"); got != "/unpin README.md " {
		t.Errorf("/unpin = %q", got)
	}
}

func TestArgCompleteTrust(t *testing.T) {
	m := newTestModel()
	ft := &fakeTrust{grants: []*trust.Grant{{ID: "g1"}, {ID: "g2"}}}
	m.sess.Trust = ft

	if got := tab(m, "/trust rev"); got != "/trust revoke " {
		t.Fatalf("/trust subcommand = %q", got)
	}
	// The trailing space moves Tab on to the id; with "all" among the
	// candidates there is no prefix to add, so Tab cycles from the first id.
	if got := tab(m, ""); got != "/trust revoke g1" {
		t.Errorf("/trust revoke <Tab> = %q, want the first id", got)
	}
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "candidates: g1  g2  all") {
		t.Errorf("ids not listed:\n%s", out)
	}
	if got := tab(m, "/trust revoke g2"); got != "/trust revoke g2 " {
		t.Errorf("/trust revoke id = %q", got)
	}
	if got := tab(m, "/trust off "); got != "/trust off " {
		t.Errorf("/trust off completed an argument: %q", got)
	}
}

// Commands with no completer — and an argument typed as @path — keep the
// @path completion Tab always gave them.
func TestArgCompleteFallsBackToPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newTestModel()
	m.sess.CWD = dir

	if got := tab(m, "/commit see @not"); got != "/commit see @notes.md" {
		t.Errorf("no-completer command = %q, want @path completion", got)
	}
	m.cycle = completeCycle{}
	if got := tab(m, "/unpin @not"); got != "/unpin @notes.md" {
		t.Errorf("@ argument = %q, want @path completion", got)
	}
}

// Every completer named in the table must be reachable by the command's own
// name, and the table must still build /help — the command list is the one
// source for both.
func TestCompletersAreOnTheirCommands(t *testing.T) {
	for _, name := range []string{"/model", "/theme", "/mode", "/logs", "/restart", "/stopjob", "/last", "/unpin", "/trust"} {
		c, ok := lookupCommand(name)
		if !ok || c.complete == nil {
			t.Errorf("%s has no completer", name)
		}
	}
}
