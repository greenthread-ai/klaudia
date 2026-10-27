package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

func TestDisplayPathsShortensUnderCWDAndHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	m := newTestModel()
	m.sess = &Session{CWD: filepath.Join(home, "proj")}
	for in, want := range map[string]string{
		"Edited " + home + "/proj/main.go (1 replacement)":   "Edited main.go (1 replacement)",
		home + "/proj/pkg/add.go\n" + home + "/proj/main.go": "pkg/add.go\nmain.go",
		"File does not exist: " + home + "/other/x.go":       "File does not exist: ~/other/x.go",
		"/opt" + home + "/proj/x":                            "/opt" + home + "/proj/x", // not a path start
		"/etc/hosts":                                         "/etc/hosts",
	} {
		if got := m.displayPaths(in); got != want {
			t.Errorf("displayPaths(%q) = %q, want %q", in, got, want)
		}
	}
}

// End to end through renderEvent: what reaches the screen is short, what /last
// keeps is not.
func TestToolResultShowsRelativePaths(t *testing.T) {
	dir := t.TempDir()
	m := newTestModel()
	m.resize(100, 30)
	m.sess.CWD = dir
	abs := filepath.Join(dir, "main.go")
	m.renderEvent(mkResult("Bash", "wrote "+abs, false))
	if out := stripANSI(m.transcript.String()); strings.Contains(out, dir) || !strings.Contains(out, "wrote main.go") {
		t.Errorf("screen shows the absolute path:\n%s", out)
	}
	if last := m.results.items[len(m.results.items)-1].content; !strings.Contains(last, abs) {
		t.Errorf("/last lost the absolute path: %q", last)
	}
}

// Battletest regression: a long absolute path in a tool header was truncated
// before it was shortened, so the prefix no longer matched and the header
// showed "⚙ Edit /private/tmp/…b8…" instead of the relative path.
func TestToolHeaderShortensBeforeTruncating(t *testing.T) {
	dir := "/very/long/working/directory/that/pushes/the/path/past/the/summary/limit/proj"
	m := newTestModel()
	m.resize(100, 30)
	m.sess.CWD = dir
	m.renderEvent(agent.Event{Type: "tool_use", ToolUseID: "e", ToolName: "Edit", Input: map[string]any{"file_path": dir + "/.klaudia/GOAL.md"}})
	m.renderEvent(agent.Event{Type: "tool_result", ToolUseID: "e", ToolName: "Edit", Content: "ok"})
	if out := stripANSI(m.transcript.String()); !strings.Contains(out, "✓ Edit .klaudia/GOAL.md") {
		t.Errorf("header = %q, want the relative path", out)
	}
}
