// Package tui is the interactive terminal frontend, built on Bubble Tea. It is
// a peer of the headless and stream-json frontends: it drives the same agent
// core (RunFunc), consumes the Emitter event stream, and resolves permission
// asks via an in-UI prompt (implementing agent.Approver).
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/permission"
)

// RunFunc drives one user turn against the agent core, threading conversation
// history and using the supplied approver/emitter.
type RunFunc func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, approver agent.Approver, emit agent.Emitter) (agent.Result, error)

// Session is mutable state shared between the TUI and the RunFunc closure, so
// slash commands like /model can change settings for subsequent turns. The
// RunFunc should read these fields fresh on each call.
type Session struct {
	Model          string // model alias or full ID ("" = default)
	PermissionMode string // for display
}

type uiState int

const (
	stateIdle uiState = iota
	stateRunning
	stateAwaitingPermission
)

// --- messages delivered from the agent goroutine ---

type eventMsg struct{ ev agent.Event }
type permissionMsg struct {
	req   agent.ApprovalRequest
	reply chan permission.Decision
}
type doneMsg struct {
	res agent.Result
	err error
}

var (
	userStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	toolStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	askStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	bannerStyle = lipgloss.NewStyle().Faint(true)
)

// Model is the Bubble Tea model for the interactive REPL.
type Model struct {
	run     RunFunc
	events  chan tea.Msg
	ctx     context.Context

	vp      viewport.Model
	input   textinput.Model
	spin    spinner.Model
	state   uiState
	ready   bool
	width   int
	height  int

	transcript strings.Builder        // rendered scrollback
	history    []anthropic.BetaMessageParam
	pending    chan permission.Decision
	pendingReq agent.ApprovalRequest
	sess       *Session
}

// New builds the model. ctx cancels in-flight turns when the program exits.
// history seeds the conversation when resuming a session (may be nil). sess is
// shared mutable settings (may be nil).
func New(ctx context.Context, run RunFunc, history []anthropic.BetaMessageParam, sess *Session) *Model {
	if sess == nil {
		sess = &Session{}
	}
	in := textinput.New()
	in.Placeholder = "Ask Klaudia… (Ctrl+C to quit)"
	in.Focus()
	in.CharLimit = 0

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := &Model{
		run:    run,
		events: make(chan tea.Msg, 256),
		ctx:    ctx,
		input:   in,
		spin:    sp,
		state:   stateIdle,
		history: history,
		sess:    sess,
	}
	m.transcript.WriteString(bannerStyle.Render("Klaudia — interactive mode. Type a prompt and press Enter.") + "\n")
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.waitForEvent())
}

// waitForEvent yields the next message from the agent goroutine.
func (m *Model) waitForEvent() tea.Cmd {
	return func() tea.Msg { return <-m.events }
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.onKey(msg)

	case eventMsg:
		m.renderEvent(msg.ev)
		return m, m.waitForEvent()

	case permissionMsg:
		m.state = stateAwaitingPermission
		m.pending = msg.reply
		m.pendingReq = msg.req
		m.appendLine(askStyle.Render(fmt.Sprintf("Allow %s? (y)es / (n)o", msg.req.ToolName)))
		return m, m.waitForEvent()

	case doneMsg:
		if msg.err != nil {
			m.appendLine(errStyle.Render("error: " + api.FriendlyError(msg.err)))
		}
		if msg.res.Messages != nil {
			m.history = msg.res.Messages
		}
		m.state = stateIdle
		m.input.Focus()
		return m, tea.Batch(textinput.Blink, m.waitForEvent())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	}

	if m.state == stateAwaitingPermission {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.answer(permission.Decision{Behavior: permission.Allow})
		case "n":
			m.answer(permission.Decision{Behavior: permission.Deny, Message: "denied by user"})
		}
		return m, nil
	}

	if msg.Type == tea.KeyEnter && m.state == stateIdle {
		prompt := strings.TrimSpace(m.input.Value())
		if prompt == "" {
			return m, nil
		}
		m.input.Reset()
		m.appendLine(userStyle.Render("› ") + prompt)

		// Slash commands are handled locally, not sent to the model.
		if strings.HasPrefix(prompt, "/") {
			return m.handleSlash(prompt)
		}

		m.state = stateRunning
		m.startTurn(prompt)
		return m, tea.Batch(m.spin.Tick, m.waitForEvent())
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// slashHelp lists the available slash commands.
const slashHelp = `Available commands:
  /help            Show this help
  /model [name]    Show or set the model (alias: haiku|sonnet|opus, or full ID)
  /status          Show the current session settings
  /clear           Clear the screen and conversation history
  /quit, /exit     Exit Klaudia`

// handleSlash dispatches a slash command. Commands run locally and never reach
// the model.
func (m *Model) handleSlash(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "/help", "/?":
		m.appendLine(bannerStyle.Render(slashHelp))
	case "/quit", "/exit":
		return m, tea.Quit
	case "/clear":
		m.transcript.Reset()
		m.history = nil
		m.appendLine(bannerStyle.Render("Cleared conversation and screen."))
	case "/model":
		if len(args) == 0 {
			cur := m.sess.Model
			if cur == "" {
				cur = "(default)"
			}
			m.appendLine(bannerStyle.Render("Model: " + cur))
		} else {
			m.sess.Model = args[0]
			m.appendLine(bannerStyle.Render("Model set to " + args[0] + " (applies to the next turn)."))
		}
	case "/status":
		mode := m.sess.PermissionMode
		if mode == "" {
			mode = "default"
		}
		model := m.sess.Model
		if model == "" {
			model = "(default)"
		}
		m.appendLine(bannerStyle.Render(fmt.Sprintf("model=%s  permission-mode=%s  messages=%d", model, mode, len(m.history))))
	default:
		m.appendLine(errStyle.Render("Unknown command " + cmd + ". Try /help."))
	}
	return m, nil
}

// answer resolves the pending permission ask.
func (m *Model) answer(d permission.Decision) {
	if m.pending != nil {
		m.pending <- d
		m.pending = nil
	}
	verb := "allowed"
	if d.Behavior != permission.Allow {
		verb = "denied"
	}
	m.appendLine(toolStyle.Render("  → " + verb))
	m.state = stateRunning
}

// startTurn runs the agent in a goroutine, delivering events via the channel.
func (m *Model) startTurn(prompt string) {
	approver := &uiApprover{events: m.events}
	emit := func(ev agent.Event) { m.events <- eventMsg{ev} }
	go func() {
		res, err := m.run(m.ctx, prompt, m.history, approver, emit)
		m.events <- doneMsg{res: res, err: err}
	}()
}

func (m *Model) renderEvent(ev agent.Event) {
	switch ev.Type {
	case "assistant":
		m.appendText(ev.Text) // streamed deltas
	case "tool_use":
		m.appendLine(toolStyle.Render(fmt.Sprintf("⚙ %s", ev.ToolName)))
	case "tool_result":
		s := ev.Content
		if len(s) > 240 {
			s = s[:240] + "…"
		}
		style := toolStyle
		if ev.IsError {
			style = errStyle
		}
		m.appendLine(style.Render("  " + strings.ReplaceAll(s, "\n", "\n  ")))
	case "compaction":
		m.appendLine(bannerStyle.Render("· " + ev.Content))
	}
}

// appendText appends inline (for streamed assistant deltas).
func (m *Model) appendText(s string) {
	m.transcript.WriteString(s)
	m.syncViewport()
}

// appendLine appends a full line.
func (m *Model) appendLine(s string) {
	m.transcript.WriteString(s + "\n")
	m.syncViewport()
}

func (m *Model) syncViewport() {
	if !m.ready {
		return
	}
	// The viewport does not wrap; wrap the content to its width (lipgloss
	// preserves ANSI styling across the wrap).
	wrapped := lipgloss.NewStyle().Width(m.vp.Width).Render(m.transcript.String())
	m.vp.SetContent(wrapped)
	m.vp.GotoBottom()
}

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	inputH := 2
	if !m.ready {
		m.vp = viewport.New(w, h-inputH)
		m.ready = true
	} else {
		m.vp.Width = w
		m.vp.Height = h - inputH
	}
	m.input.Width = w - 4
	m.syncViewport()
}

func (m *Model) View() string {
	if !m.ready {
		return "initializing…"
	}
	var bottom string
	switch m.state {
	case stateRunning:
		bottom = m.spin.View() + " working…"
	case stateAwaitingPermission:
		bottom = askStyle.Render(fmt.Sprintf("Allow %s? (y/n)", m.pendingReq.ToolName))
	default:
		bottom = m.input.View()
	}
	return m.vp.View() + "\n" + bottom
}

// uiApprover implements agent.Approver by asking the UI and blocking for the
// user's decision.
type uiApprover struct {
	events chan tea.Msg
}

func (a *uiApprover) Approve(ctx context.Context, req agent.ApprovalRequest) permission.Decision {
	reply := make(chan permission.Decision, 1)
	a.events <- permissionMsg{req: req, reply: reply}
	select {
	case <-ctx.Done():
		return permission.Decision{Behavior: permission.Deny, Message: "cancelled"}
	case d := <-reply:
		return d
	}
}

// Run starts the interactive program and blocks until the user quits. history
// seeds a resumed conversation (may be nil); sess holds mutable settings shared
// with the RunFunc closure (may be nil).
func Run(ctx context.Context, run RunFunc, history []anthropic.BetaMessageParam, sess *Session) error {
	p := tea.NewProgram(New(ctx, run, history, sess), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
