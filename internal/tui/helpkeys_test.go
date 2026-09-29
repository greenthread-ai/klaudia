package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sameCmd(a, b tea.Cmd) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func TestHelpColumnsAlign(t *testing.T) {
	col := 2 + helpColumn + 1 // rune index where every description starts
	lines := strings.Split(slashHelp(), "\n")
	seen := 0
	for i := 0; i < len(lines); i++ {
		if lines[i] == "Keys:" {
			break
		}
		ln := []rune(lines[i])
		if !strings.HasPrefix(lines[i], "  /") && !strings.HasPrefix(lines[i], "  !") {
			continue
		}
		seen++
		if len(ln) > col && ln[col-1] == ' ' && ln[col] != ' ' {
			continue // usage and description on one line, description aligned
		}
		// Otherwise the usage is too long: it stands alone and the next line
		// carries the description at the same column.
		if len(ln)-2 <= helpColumn {
			t.Errorf("short usage not aligned: %q", lines[i])
			continue
		}
		if i+1 >= len(lines) {
			t.Errorf("long usage %q has no description line", lines[i])
			continue
		}
		next := []rune(lines[i+1])
		if len(next) <= col || strings.TrimSpace(string(next[:col])) != "" || next[col] == ' ' {
			t.Errorf("description after long usage %q is not at column %d: %q", lines[i], col, lines[i+1])
		}
		i++
	}
	if seen == 0 {
		t.Fatal("no command lines found in /help")
	}
}

func TestHelpDocumentsSubcommands(t *testing.T) {
	h := slashHelp()
	for _, want := range []string{"clear", "/logs stop", "revoke <id|all>", "ls", "Ctrl+W", "Alt+←", "Ctrl+Z", "Ctrl+L", "Ctrl+D"} {
		if !strings.Contains(h, want) {
			t.Errorf("/help does not mention %q", want)
		}
	}
}

func TestCtrlKeys(t *testing.T) {
	m := newTestModel()
	m.state = stateIdle

	if _, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlZ}); !sameCmd(cmd, tea.Suspend) {
		t.Error("Ctrl+Z should suspend")
	}
	if _, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlL}); !sameCmd(cmd, tea.ClearScreen) {
		t.Error("Ctrl+L should clear the screen")
	}
	if _, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlD}); !sameCmd(cmd, tea.Quit) {
		t.Error("Ctrl+D on an empty idle prompt should quit")
	}

	m.input.SetValue("draft")
	if _, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlD}); sameCmd(cmd, tea.Quit) {
		t.Error("Ctrl+D with text in the prompt must not quit")
	}
	m.input.SetValue("")
	m.state = stateRunning
	if _, cmd := m.onKey(tea.KeyMsg{Type: tea.KeyCtrlD}); sameCmd(cmd, tea.Quit) {
		t.Error("Ctrl+D while a turn runs must not quit")
	}
}

func TestPlanRejectsUnknownArgument(t *testing.T) {
	m := newTestModel()
	m.sess.PermissionMode = "default"
	m.handleSlash("/plan xyz")
	if m.sess.PermissionMode != "default" {
		t.Errorf("/plan xyz changed the mode to %q", m.sess.PermissionMode)
	}
	if out := visibleText(m.transcript.String()); !strings.Contains(out, "usage: /plan") {
		t.Errorf("/plan xyz should print usage, got:\n%s", out)
	}
	m.handleSlash("/plan")
	if m.sess.PermissionMode != "plan" {
		t.Errorf("/plan should enter plan mode, got %q", m.sess.PermissionMode)
	}
	m.handleSlash("/plan off")
	if m.sess.PermissionMode != "default" {
		t.Errorf("/plan off should leave plan mode, got %q", m.sess.PermissionMode)
	}
}

func TestErrorsRejectsNonNumber(t *testing.T) {
	m := newTestModel()
	m.listErrors([]string{"foo"})
	if out := visibleText(m.transcript.String()); !strings.Contains(out, "usage: /errors") {
		t.Errorf("/errors foo should print usage, got:\n%s", out)
	}
}

func TestResolveAddDir(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.Mkdir(filepath.Join(cwd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cwd, "f.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := resolveAddDir(cwd, "sub"); err != nil || got != filepath.Join(cwd, "sub") {
		t.Errorf("relative: got %q, %v", got, err)
	}
	if got, err := resolveAddDir(cwd, "~/notes"); err != nil || got != filepath.Join(home, "notes") {
		t.Errorf("tilde: got %q, %v", got, err)
	}
	if _, err := resolveAddDir(cwd, "missing"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing dir: err = %v", err)
	}
	if _, err := resolveAddDir(cwd, "f.txt"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file: err = %v", err)
	}
}

func TestAddDirRefusesMissingPath(t *testing.T) {
	m := newTestModel()
	m.sess.CWD = t.TempDir()
	m.handleSlash("/add-dir nope")
	if len(m.sess.ExtraDirs) != 0 {
		t.Errorf("a missing directory was added: %v", m.sess.ExtraDirs)
	}
}
