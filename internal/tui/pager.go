package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
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

// diffArgs builds the git argv for /diff. Our colour flag goes before the
// user's own arguments so an explicit --color/--no-color of theirs still wins.
// With colour off we say --no-color rather than relying on git's auto mode,
// because a color.ui = always in the user's git config would otherwise ignore
// NO_COLOR.
func diffArgs(colour bool, args []string) []string {
	flag := "--no-color"
	if colour {
		flag = "--color=always"
	}
	return append([]string{"diff", flag}, args...)
}

// diffColour reports whether /diff should ask git for colour: not when the
// user set NO_COLOR, and not on a terminal that cannot show it. git cannot
// decide for itself — its stdout is a pipe to us, so its auto mode is always
// off.
func diffColour(env func(string) string) bool {
	if env("NO_COLOR") != "" {
		return false
	}
	return lipgloss.ColorProfile() != termenv.Ascii
}

// showDiff backs /diff. It takes the same route as showLong — a long diff goes
// to $PAGER, a short one prints — but prints the diff as git wrote it rather
// than through showLong's inline styling: a muted foreground would fight git's
// own colours, and a diff that touches Markdown must not be rendered as
// Markdown.
func (m *Model) showDiff(args []string) tea.Cmd {
	out, err := gitOutput(m.sess.CWD, diffArgs(diffColour(os.Getenv), args)...)
	switch {
	case err != nil:
		msg := strings.TrimSpace(out)
		if msg == "" {
			msg = err.Error()
		}
		m.appendLine(errStyle.Render("git diff: " + msg))
		return nil
	case strings.TrimSpace(out) == "":
		m.appendLine(bannerStyle.Render("No changes."))
		return nil
	}
	if m.shouldPage(out) {
		if cmd, err := m.pageText("diff", out); err == nil {
			return cmd
		}
		// No pager, or the temp file failed: fall through and print.
	}
	m.appendLine(strings.TrimRight(out, "\n"))
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
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return pagerDoneMsg{err: err}
	})
}
