package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sameCmd(a, b tea.Cmd) bool {
	return a != nil && b != nil && reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// Raw mode delivers Ctrl+Z, Ctrl+L and Ctrl+D as keys; they did nothing.
// (PR #89.)
func TestCtrlKeys(t *testing.T) {
	ctrl := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
	m := newTestModel()
	if _, cmd := m.onKey(ctrl('z')); !sameCmd(cmd, tea.Suspend) {
		t.Error("Ctrl+Z should suspend")
	}
	if _, cmd := m.onKey(ctrl('l')); !sameCmd(cmd, tea.ClearScreen) {
		t.Error("Ctrl+L should clear the screen")
	}
	m.onKey(ctrl('d'))
	if !m.quitting {
		t.Error("Ctrl+D on an empty idle prompt should quit")
	}

	m = newTestModel()
	m.input.SetValue("draft")
	m.onKey(ctrl('d'))
	if m.quitting {
		t.Error("Ctrl+D with text in the prompt must not quit")
	}
	m = newTestModel()
	m.state = stateRunning
	m.onKey(ctrl('d'))
	if m.quitting {
		t.Error("Ctrl+D while a turn runs must not quit")
	}
}

// Arguments a command would have ignored are refused with its usage.
func TestPlanAndErrorsRejectUnknownArguments(t *testing.T) {
	m := newTestModel()
	m.sess.PermissionMode = "autonomous"
	m.handleSlash("/plan xyz")
	if m.sess.PermissionMode != "autonomous" || !strings.Contains(visibleText(m.transcript.String()), "usage: /plan") {
		t.Errorf("/plan xyz: mode %q; want it unchanged and the usage shown", m.sess.PermissionMode)
	}
	m.handleSlash("/plan")
	if m.sess.PermissionMode != "plan" {
		t.Errorf("/plan should enter plan mode, got %q", m.sess.PermissionMode)
	}
	m.handleSlash("/plan off")
	if m.sess.PermissionMode == "plan" {
		t.Error("/plan off should leave plan mode")
	}

	m = newTestModel()
	m.listErrors([]string{"foo"})
	if !strings.Contains(visibleText(m.transcript.String()), "usage: /errors") {
		t.Error("/errors foo should print usage")
	}
}

func TestResolveAddDir(t *testing.T) {
	cwd, home := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	os.Mkdir(filepath.Join(cwd, "sub"), 0o755)
	os.Mkdir(filepath.Join(home, "notes"), 0o755)
	os.WriteFile(filepath.Join(cwd, "f.txt"), nil, 0o644)

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

	m := newTestModel()
	m.sess.CWD = cwd
	m.handleSlash("/add-dir nope")
	if len(m.sess.ExtraDirs) != 0 {
		t.Errorf("a missing directory was added: %v", m.sess.ExtraDirs)
	}
}
