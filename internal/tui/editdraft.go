package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Ctrl+G opens the current prompt draft in the user's editor. A long or
// carefully-structured prompt is painful to compose in a one-line-ish box with
// no editor motions; handing it to $EDITOR is the same instinct as `git commit`
// dropping you into one, and Bubble Tea v1's tea.ExecProcess makes it cheap —
// it suspends the TUI, gives the child the real terminal, and restores the UI
// (repaint + cursor) when the child exits, so no manual terminal juggling is
// needed.

// draftEditorFallbacks is the list of editors tried, in order, when neither
// $VISUAL nor $EDITOR is set or usable. It is a var so tests can point it at a
// stub without depending on what happens to be installed. vi is POSIX-mandated
// and present on essentially every box; nano is the common friendlier default.
var draftEditorFallbacks = []string{"vi", "nano", "nvim", "vim"}

// draftEditorCommand resolves the editor used to edit a scratch draft file.
//
// It follows the same $VISUAL → $EDITOR order as editorCommand (which backs
// /open), then, unlike /open, falls back to a plain editor found on PATH so
// Ctrl+G still works when neither variable is set — the issue asks for a
// sensible default rather than a hard failure. env is injected so resolution
// stays testable without touching the process environment.
func draftEditorCommand(env func(string) string, path string) (*exec.Cmd, bool) {
	editor := strings.TrimSpace(env("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(env("EDITOR"))
	}
	if editor != "" {
		// $EDITOR may carry flags ("code -w", "emacsclient -t"); keep them and
		// append the path last, matching editorCommand.
		fields := strings.Fields(editor)
		if bin, err := exec.LookPath(fields[0]); err == nil {
			args := append(append([]string{}, fields[1:]...), path)
			return exec.Command(bin, args...), true
		}
	}
	for _, fallback := range draftEditorFallbacks {
		if bin, err := exec.LookPath(fallback); err == nil {
			return exec.Command(bin, path), true
		}
	}
	return nil, false
}

// writeDraftFile writes content to a fresh temp file and returns its path. The
// caller owns removal (the ExecProcess callback delivers the path back for
// exactly that).
func writeDraftFile(content string) (string, error) {
	f, err := os.CreateTemp("", "klaudia-draft-*.md")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// readDraftFile reads an edited draft back, dropping the trailing newline(s)
// editors add so the box does not accumulate a blank line on every round trip.
// Submit trims the box anyway (readInput), so this only changes what is shown.
func readDraftFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// editDraftDoneMsg reports that the external editor exited, carrying the temp
// file path so the draft can be read back in and the file removed.
type editDraftDoneMsg struct {
	path string
	err  error
}

// editDraft backs Ctrl+G: it writes the current draft to a temp file and opens
// it in the user's editor via tea.ExecProcess. The expanded prompt value is
// written (paste chips resolved to their payloads) so what the user edits is
// the real text that would be sent. On any setup failure it prints a brief
// message and returns nil — Ctrl+G must never leave the prompt broken.
func (m *Model) editDraft() tea.Cmd {
	path, err := writeDraftFile(m.promptValue())
	if err != nil {
		m.appendLine(errStyle.Render("edit: " + err.Error()))
		return nil
	}
	cmd, ok := draftEditorCommand(os.Getenv, path)
	if !ok {
		os.Remove(path)
		m.appendLine(errStyle.Render("edit: set $EDITOR or $VISUAL (no vi/nano on PATH either)"))
		return nil
	}
	m.appendLine(bannerStyle.Render("Editing draft in your editor…"))
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editDraftDoneMsg{path: path, err: err}
	})
}

// applyEditedDraft is the editDraftDoneMsg handler. It always removes the temp
// file. On an editor error it keeps the original draft untouched and reports
// the failure; otherwise it reads the edited content back into the box.
func (m *Model) applyEditedDraft(msg editDraftDoneMsg) {
	if msg.path != "" {
		defer os.Remove(msg.path)
	}
	if msg.err != nil {
		m.appendLine(errStyle.Render("edit: " + msg.err.Error()))
		return
	}
	if msg.path == "" {
		return
	}
	text, err := readDraftFile(msg.path)
	if err != nil {
		m.appendLine(errStyle.Render("edit: " + err.Error()))
		return
	}
	m.input.SetValue(text)
	m.input.CursorEnd()
	m.syncInputHeight()
	// The box changed wholesale; drop any paste payloads it no longer refers to.
	m.reconcilePastes()
}
