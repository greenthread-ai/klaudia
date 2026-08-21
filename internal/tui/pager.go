package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
