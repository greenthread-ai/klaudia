package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// §14: natural language and shell commands should coexist.
//
// The user should not have to decide whether they are "in the AI program" or
// "in the terminal". They ask Klaudia to investigate, then run `git diff`
// themselves to look, then tell Klaudia what to do about it — and the middle
// step should not mean leaving.
//
// A leading `!` runs the rest of the line directly, matching Claude Code. The
// output goes into the conversation as well as the screen, so "revert the API
// change" refers to something the model can actually see. Without that, running
// a command here would be strictly worse than switching to another terminal.

// bangResultMsg carries a finished direct command back to the update loop.
type bangResultMsg struct {
	command string
	output  string
	code    int
	took    time.Duration
	err     error
}

// isBang reports whether a line is a direct shell command.
func isBang(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "!")
}

// bangCommand strips the marker.
func bangCommand(line string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimLeft(line, " \t"), "!"))
}

// runBang executes a direct command.
//
// It is the user's own instruction, so the host boundary does not apply — they
// typed it, and asking them to approve their own command would be absurd. What
// it does do is classify and *show* the zone, so a line pasted from somewhere
// else still says what it touches before it runs.
func (m *Model) runBang(line string) (tea.Model, tea.Cmd) {
	cmd := bangCommand(line)
	if cmd == "" {
		m.appendLine(hintStyle.Render("  ! runs a shell command directly, e.g. !git status"))
		return m, nil
	}
	if reason, blocked := sandbox.TTYRequired(cmd); blocked {
		// Klaudia has no terminal to give it either way, and hanging the UI on
		// a program waiting for a keypress would be worse here than in a tool
		// call: the user is watching.
		m.appendLine(errStyle.Render("  " + reason))
		return m, nil
	}

	m.appendLine(bangEchoStyle.Render("$ " + cmd))
	if note := m.bangZoneNote(cmd); note != "" {
		m.appendLine(hintStyle.Render("  " + note))
	}

	dir := m.sess.CWD
	exec := m.executor
	if exec == nil {
		exec = sandbox.NewLocal()
	}
	started := time.Now()
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), bangTimeout)
		defer cancel()
		resp, err := exec.Run(ctx, sandbox.Request{Command: cmd, WorkingDir: dir})
		out := resp.Stdout
		if resp.Stderr != "" {
			if out != "" && !strings.HasSuffix(out, "\n") {
				out += "\n"
			}
			out += resp.Stderr
		}
		return bangResultMsg{
			command: cmd, output: out, code: resp.ExitCode,
			took: time.Since(started), err: err,
		}
	}
}

// bangTimeout bounds a direct command. Generous, because the user is watching
// and can Ctrl+C; a long build typed by hand is a reasonable thing to do.
const bangTimeout = 10 * time.Minute

// bangZoneNote describes what a command touches, when that is worth saying.
//
// Not a gate — the user typed it. But a line pasted from a README that turns
// out to write to /etc is worth one sentence before it happens rather than
// after.
func (m *Model) bangZoneNote(cmd string) string {
	if m.sess == nil {
		return ""
	}
	as := trust.ClassifyCommandIn(cmd, m.sess.CWD, m.trustRoots())
	concerns, needs := as.NeedsAgreement()
	if !needs || len(concerns) == 0 {
		return ""
	}
	return "this " + as.Summary() + " — running it because you asked"
}

// trustRoots is the classifier's view of this session, for the zone note.
func (m *Model) trustRoots() trust.Roots {
	if m.sess == nil {
		return trust.Roots{}
	}
	home, _ := os.UserHomeDir()
	roots := append([]string{m.sess.CWD}, m.sess.ExtraDirs...)
	return trust.NewRoots(home, roots...)
}

// onBangResult renders a finished direct command.
func (m *Model) onBangResult(msg bangResultMsg) tea.Cmd {
	body := strings.TrimRight(msg.output, "\n")
	switch {
	case msg.err != nil:
		m.appendLine(errStyle.Render("  " + msg.err.Error()))
	case body == "":
		m.appendLine(toolStyle.Render(fmt.Sprintf("  (no output · %s)", fmtDuration(msg.took))))
	default:
		clipped, dropped := clipPreview(body, maxPreviewLines, maxPreviewRunes)
		seq := m.results.add(toolResult{
			tool: "shell", command: msg.command, at: time.Now(),
			content: body, isError: msg.code != 0,
		})
		if dropped > 0 {
			clipped += fmt.Sprintf("\n…  %s", hintStyle.Render(
				fmt.Sprintf("(%d more lines · /last %d for full output)", dropped, seq)))
		}
		style := toolStyle
		if msg.code != 0 {
			style = errStyle
		}
		m.appendLine(style.Render("  " + strings.ReplaceAll(clipped, "\n", "\n  ")))
	}
	if msg.code != 0 && msg.err == nil {
		m.appendLine(errStyle.Render(fmt.Sprintf("  [exit %d]", msg.code)))
	}

	// The model has to see this, or the next instruction ("revert that") has no
	// referent. Recorded as conversation rather than sent as a turn: the user
	// ran a command, they did not ask a question.
	m.recordShellContext(msg)
	m.setState(stateIdle)
	return nil
}

// recordShellContext appends the command and its output to the conversation so
// the next natural-language instruction can refer to it.
func (m *Model) recordShellContext(msg bangResultMsg) {
	out := msg.output
	// The model does not need four thousand lines of a build log to understand
	// what the user was looking at.
	if clipped, dropped := clipPreview(out, 60, 4000); dropped > 0 {
		out = clipped + fmt.Sprintf("\n… (%d more lines)", dropped)
	}
	m.pendingShellContext = append(m.pendingShellContext, fmt.Sprintf(
		"The user ran this in the terminal:\n\n```\n$ %s\n%s\n```\n(exit %d)",
		msg.command, strings.TrimRight(out, "\n"), msg.code))
}

// takeShellContext drains the shell transcript into the next prompt.
func (m *Model) takeShellContext() string {
	if len(m.pendingShellContext) == 0 {
		return ""
	}
	body := strings.Join(m.pendingShellContext, "\n\n")
	m.pendingShellContext = nil
	return body
}
