package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	m.renderEvent(mkResult("Edit", "Edited "+abs+" (1 replacement)", false))
	if out := stripANSI(m.transcript.String()); strings.Contains(out, dir) || !strings.Contains(out, "Edited main.go") {
		t.Errorf("screen shows the absolute path:\n%s", out)
	}
	if last := m.results.items[len(m.results.items)-1].content; !strings.Contains(last, abs) {
		t.Errorf("/last lost the absolute path: %q", last)
	}
}
