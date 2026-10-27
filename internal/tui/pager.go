package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Long output is handed to the user's own pager rather than shown in a pager we
// built. $PAGER already does paging, incremental search, regex, line numbers and
// selection-copy, it is already configured the way the user likes, and reaching
// for it is the same instinct as piping the command to less by hand. Building an
// in-app equivalent would also fight the inline renderer, which exists precisely
// to stop Klaudia from owning the scroll region.

// pagerDoneMsg reports that the pager exited so the temp file can be removed.
type pagerDoneMsg struct {
	path string
	err  error
}

// pagerCommand resolves the pager to use. env is injected so this stays testable
// without touching the process environment.
func pagerCommand(env func(string) string, path string) (*exec.Cmd, bool) {
	candidates := []string{env("PAGER"), "less", "more"}
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		fields := strings.Fields(c)
		bin, err := exec.LookPath(fields[0])
		if err != nil {
			continue
		}
		args := append(append([]string{}, fields[1:]...), path)
		// -R keeps our ANSI colouring intact; skip it if the user already
		// expressed a preference through $LESS.
		if filepath.Base(fields[0]) == "less" && !strings.Contains(env("LESS"), "R") {
			args = append([]string{"-R"}, args...)
		}
		return exec.Command(bin, args...), true
	}
	return nil, false
}

// shouldPage reports whether text is long enough to be worth a pager.
func (m *Model) shouldPage(text string) bool {
	limit := m.height * 2
	if limit <= 0 {
		limit = 200
	}
	return strings.Count(text, "\n")+1 > limit
}

// pageText opens text in $PAGER. The content goes to a temp file rather than the
// pager's stdin, because the pager needs stdin for keystrokes.
func (m *Model) pageText(name, text string) (tea.Cmd, error) {
	f, err := os.CreateTemp("", "klaudia-"+name+"-*.txt")
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	f.Close()

	cmd, ok := pagerCommand(os.Getenv, f.Name())
	if !ok {
		os.Remove(f.Name())
		return nil, errNoPager
	}
	path := f.Name()
	return m.execProcess(cmd, func(err error) tea.Msg {
		return pagerDoneMsg{path: path, err: err}
	}), nil
}

var errNoPager = pagerUnavailable{}

type pagerUnavailable struct{}

func (pagerUnavailable) Error() string { return "no pager found on PATH" }

// showLong pages text when it is long and a pager exists, and otherwise prints
// it inline. Printing is a perfectly good fallback now that output lands in the
// terminal's own scrollback.
func (m *Model) showLong(name, text string) tea.Cmd {
	if m.shouldPage(text) {
		if cmd, err := m.pageText(name, text); err == nil {
			return cmd
		}
		// No pager, or the temp file failed: fall through and print.
	}
	if strings.Contains(text, "```") {
		m.appendMarkdown(text)
	} else {
		m.appendLine(toolStyle.Render(text))
	}
	return nil
}

// editorCommand builds the argv to open ref at its line. Editors disagree about
// how to say "line N", so the common ones are special-cased and everything else
// falls back to "$EDITOR <path>".
func editorCommand(env func(string) string, root string, ref fileRef) (*exec.Cmd, bool) {
	editor := strings.TrimSpace(env("VISUAL"))
	if editor == "" {
		editor = strings.TrimSpace(env("EDITOR"))
	}
	if editor == "" {
		return nil, false
	}
	fields := strings.Fields(editor)
	bin, err := exec.LookPath(fields[0])
	if err != nil {
		return nil, false
	}
	path := ref.Path
	if !filepath.IsAbs(path) && root != "" {
		path = filepath.Join(root, path)
	}
	args := append([]string{}, fields[1:]...)
	switch base := filepath.Base(fields[0]); {
	case ref.Line <= 0:
		args = append(args, path)
	case base == "vim" || base == "nvim" || base == "vi" || base == "emacs" || base == "nano":
		args = append(args, "+"+strconv.Itoa(ref.Line), path)
	case base == "code" || base == "cursor" || base == "codium":
		args = append(args, "-g", ref.String())
	case base == "hx" || base == "helix" || base == "subl" || base == "idea":
		args = append(args, ref.String())
	default:
		args = append(args, path)
	}
	return exec.Command(bin, args...), true
}

// openInEditor backs /open. It resolves a path:line reference — the form every
// compiler and stack trace prints — and hands it to $EDITOR.
func (m *Model) openInEditor(args []string) tea.Cmd {
	if len(args) == 0 {
		m.appendLine(errStyle.Render("usage: /open <path[:line[:col]]>"))
		return nil
	}
	root := m.rootDir()
	var extra []string
	if m.sess != nil {
		extra = m.sess.ExtraDirs
	}
	ref, ok := parseFileRef(root, args[0], extra)
	if !ok {
		m.appendLine(errStyle.Render("open: no such file: " + args[0]))
		return nil
	}
	cmd, ok := editorCommand(os.Getenv, root, ref)
	if !ok {
		m.appendLine(errStyle.Render("open: set $EDITOR (or $VISUAL) first"))
		return nil
	}
	m.noteRecentPath(ref.Path)
	m.appendLine(bannerStyle.Render("Opening " + ref.String() + "…"))
	return m.execProcess(cmd, func(err error) tea.Msg {
		return pagerDoneMsg{err: err}
	})
}

// showDiff is /diff: git's own diff, in git's colours, paged when it is long
// (as /last pages output) and printed when it is short. It used to print the
// whole diff, uncoloured, into scrollback — a refactor's diff was hundreds of
// lines between the user and their prompt.
//
// A bare /diff also names untracked files: `git diff` leaves them out, and a
// file Klaudia created is often the change that matters.
func (m *Model) showDiff(args []string) tea.Cmd {
	// git's colours unless the terminal has none (NO_COLOR included), the
	// same test the Markdown style uses.
	color := "--color=always"
	if noColor() {
		color = "--no-color"
	}
	out, err := gitOutput(m.sess.CWD, append([]string{"diff", color}, args...)...)
	if err != nil {
		m.appendLine(errStyle.Render("git diff: " + strings.TrimSpace(out) + " " + err.Error()))
		return nil
	}
	if len(args) == 0 {
		if untracked, err := gitOutput(m.sess.CWD, "ls-files", "--others", "--exclude-standard"); err == nil {
			if files := strings.Fields(untracked); len(files) > 0 {
				note := "New, untracked (not in the diff): " + shortList(files, 8)
				if strings.TrimSpace(out) == "" {
					m.appendLine(bannerStyle.Render(note))
					return nil
				}
				out = strings.TrimRight(out, "\n") + "\n\n" + note
			}
		}
	}
	if strings.TrimSpace(out) == "" {
		m.appendLine(bannerStyle.Render("No changes."))
		return nil
	}
	if m.shouldPage(out) {
		if cmd, err := m.pageText("diff", out); err == nil {
			return cmd
		}
	}
	m.appendLine(strings.TrimRight(out, "\n"))
	return nil
}
