// Package tui is the interactive terminal frontend, built on Bubble Tea. It is
// a peer of the headless and stream-json frontends: it drives the same agent
// core (RunFunc), consumes the Emitter event stream, and resolves permission
// asks via an in-UI prompt (implementing agent.Approver).
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/memory"
	"github.com/greenthread/klaudia/internal/native/search"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/tools"
)

// RunFunc drives one user turn against the agent core, threading conversation
// history and using the supplied approver, asker, and emitter.
type RunFunc func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, approver agent.Approver, asker tools.Asker, planner tools.Planner, emit agent.Emitter) (agent.Result, error)

// Session is mutable state shared between the TUI and the RunFunc closure, so
// slash commands like /model can change settings for subsequent turns. The
// RunFunc should read these fields fresh on each call.
type Session struct {
	Model          string         // model alias or full ID ("" = default)
	ResolvedModel  string         // concrete model id for display
	PermissionMode string         // live mode (ExitPlanMode flips it out of "plan")
	Memory         *memory.Store  // backs /memory (may be nil)
	MCPSummary     []string       // lines for /mcp ("server: tool1, tool2")
	Goal           string         // standing goal re-injected each turn (Ralph-style)
	Skills         []SkillCommand // user-defined skills dispatched as /<name>

	// Render-only context for /config and /context (set once at startup).
	Provider    string   // resolved provider ("anthropic" | "openai" | …)
	SandboxMode string   // resolved sandbox mode ("local" | "os" | "container")
	CWD         string   // working directory
	GitBranch   string   // current git branch (may be "")
	Agents      []AgentInfo // built-in sub-agent types, for /agents
	ExtraDirs   []string // additional working dirs added via /add-dir

	// Compact, if set, runs a model-based compaction of the given history and
	// returns the replacement history plus the summary. Backs /compact.
	Compact CompactFunc
	// Doctor, if set, returns a rendered environment diagnostic. Backs /doctor.
	Doctor func() string
}

// CompactFunc summarizes the conversation history via the model, returning the
// replacement history and the summary text.
type CompactFunc func(ctx context.Context, history []anthropic.BetaMessageParam) (newHistory []anthropic.BetaMessageParam, summary string, err error)

// AgentInfo is the model-facing summary of a sub-agent type, shown by /agents.
type AgentInfo struct {
	Name        string
	Description string
}

// SkillCommand is a user-defined skill exposed as a /<name> command in the TUI.
// Render returns the skill body with $ARGUMENTS substituted; the TUI submits the
// rendered text as the turn's prompt.
type SkillCommand struct {
	Name        string
	Description string
	Render      func(arguments string) string
}

type uiState int

const (
	stateIdle uiState = iota
	stateRunning
	stateAwaitingPermission
	stateAwaitingAnswer
	stateAwaitingPlan
	stateAwaitingConfirm
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
type compactDoneMsg struct {
	history []anthropic.BetaMessageParam
	summary string
	err     error
}
type askMsg struct {
	question string
	options  []tools.AskOption
	reply    chan string
}
type planMsg struct {
	plan  string
	reply chan bool
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
	// Session-scoped allow/deny rules added via "allow always" or /allow,/deny.
	// Accessed only from the UI goroutine (Update) to avoid races.
	sessionAllow []permission.Rule
	sessionDeny  []permission.Rule
	// Cumulative session stats for /stats.
	statTurns int
	statIn    int64
	statOut   int64
	// Pending AskUserQuestion.
	askReply    chan string
	askOptions  []tools.AskOption
	askQuestion string
	// Pending ExitPlanMode approval.
	planReply chan bool
	// Pending /commit-style confirmation: run on "y", returns a result line.
	confirmAction func() string
	// Input history: submitted prompts (newest last), navigated with Up/Down.
	// histPos == len(inputHistory) means "not navigating" (editing a fresh line).
	inputHistory []string
	histPos      int
	histDraft    string // the in-progress line stashed when navigating up
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
//
// INVARIANT: exactly one waitForEvent command must be outstanding at any time.
// Bubble Tea runs commands in their own goroutines, so two outstanding readers
// would race on the events channel and deliver streamed deltas out of order.
// Init arms the single reader; every channel-event case in Update re-arms it
// one-for-one. Do not arm an extra one from a KeyMsg path (e.g. submit).
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
		// Auto-resolve against session rules before prompting.
		if permission.MatchAny(m.sessionDeny, msg.req.ToolName, msg.req.Specifier) {
			msg.reply <- permission.Decision{Behavior: permission.Deny, Message: "denied by session rule"}
			return m, m.waitForEvent()
		}
		if permission.MatchAny(m.sessionAllow, msg.req.ToolName, msg.req.Specifier) {
			msg.reply <- permission.Decision{Behavior: permission.Allow}
			return m, m.waitForEvent()
		}
		m.state = stateAwaitingPermission
		m.pending = msg.reply
		m.pendingReq = msg.req
		label := msg.req.ToolName
		if msg.req.Specifier != "" {
			label += " (" + msg.req.Specifier + ")"
		}
		m.appendLine(askStyle.Render(fmt.Sprintf("Allow %s? (y)es once / (a)lways / (n)o", label)))
		return m, m.waitForEvent()

	case askMsg:
		m.state = stateAwaitingAnswer
		m.askReply = msg.reply
		m.askOptions = msg.options
		m.askQuestion = msg.question
		m.appendLine(askStyle.Render("? " + msg.question))
		for i, o := range msg.options {
			line := fmt.Sprintf("  %d) %s", i+1, o.Label)
			if o.Description != "" {
				line += " — " + o.Description
			}
			m.appendLine(toolStyle.Render(line))
		}
		return m, m.waitForEvent()

	case planMsg:
		m.state = stateAwaitingPlan
		m.planReply = msg.reply
		m.appendLine(askStyle.Render("Proposed plan:"))
		m.appendLine(msg.plan)
		m.appendLine(askStyle.Render("Approve and start implementing? (y)es / (n)o"))
		return m, m.waitForEvent()

	case doneMsg:
		if msg.err != nil {
			m.appendLine(errStyle.Render("error: " + api.FriendlyError(msg.err)))
		}
		if msg.res.Messages != nil {
			m.history = msg.res.Messages
		}
		m.statTurns += msg.res.NumTurns
		m.statIn += msg.res.InputTokens
		m.statOut += msg.res.OutputTokens
		m.state = stateIdle
		m.input.Focus()
		return m, tea.Batch(textinput.Blink, m.waitForEvent())

	case compactDoneMsg:
		if msg.err != nil {
			m.appendLine(errStyle.Render("compact: " + api.FriendlyError(msg.err)))
		} else {
			m.history = msg.history
			m.appendLine(bannerStyle.Render("Compacted conversation. Summary:\n" + strings.TrimSpace(msg.summary)))
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

	if m.state == stateAwaitingPlan {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.planReply <- true
			m.planReply = nil
			m.sess.PermissionMode = string(permission.ModeAcceptEdits) // leave plan mode
			m.appendLine(toolStyle.Render("  → approved; plan mode off (acceptEdits)"))
			m.state = stateRunning
		case "n":
			m.planReply <- false
			m.planReply = nil
			m.appendLine(toolStyle.Render("  → not approved; staying in plan mode"))
			m.state = stateRunning
		}
		return m, nil
	}

	if m.state == stateAwaitingConfirm {
		switch strings.ToLower(msg.String()) {
		case "y":
			action := m.confirmAction
			m.confirmAction = nil
			m.state = stateIdle
			if action != nil {
				m.appendLine(bannerStyle.Render(action()))
			}
		case "n":
			m.confirmAction = nil
			m.state = stateIdle
			m.appendLine(toolStyle.Render("  → cancelled"))
		}
		return m, nil
	}

	if m.state == stateAwaitingAnswer {
		// Digit keys 1..N select an option.
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if n := int(s[0] - '0'); n <= len(m.askOptions) {
				choice := m.askOptions[n-1].Label
				m.askReply <- choice
				m.askReply = nil
				m.appendLine(toolStyle.Render("  → " + choice))
				m.state = stateRunning
			}
		}
		return m, nil
	}

	if m.state == stateAwaitingPermission {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.answer(permission.Decision{Behavior: permission.Allow})
		case "a":
			// Remember this tool+specifier for the rest of the session.
			rule := permission.Rule{Tool: m.pendingReq.ToolName, Specifier: m.pendingReq.Specifier}
			m.sessionAllow = append(m.sessionAllow, rule)
			m.appendLine(toolStyle.Render("  → always allow " + permission.FormatRule(rule) + " (this session)"))
			m.answer(permission.Decision{Behavior: permission.Allow})
		case "n":
			m.answer(permission.Decision{Behavior: permission.Deny, Message: "denied by user"})
		}
		return m, nil
	}

	// @-path completion on Tab (only when editing an idle line).
	if m.state == stateIdle && msg.Type == tea.KeyTab {
		m.completeAtPath()
		return m, nil
	}

	// Input history navigation (only on a fresh idle line).
	if m.state == stateIdle && (msg.Type == tea.KeyUp || msg.Type == tea.KeyDown) {
		m.navigateHistory(msg.Type == tea.KeyUp)
		return m, nil
	}

	if msg.Type == tea.KeyEnter && m.state == stateIdle {
		prompt := strings.TrimSpace(m.input.Value())
		if prompt == "" {
			return m, nil
		}
		m.input.Reset()
		m.pushHistory(prompt)
		m.appendLine(userStyle.Render("› ") + prompt)

		// Slash commands are handled locally, not sent to the model.
		if strings.HasPrefix(prompt, "/") {
			return m.handleSlash(prompt)
		}

		m.state = stateRunning
		m.startTurn(prompt)
		// Do NOT arm another waitForEvent here: exactly one channel reader must
		// be outstanding (Init armed it; each channel-event handler re-arms it).
		// A second reader would race and deliver streamed deltas out of order.
		return m, m.spin.Tick
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// slashHelp lists the available slash commands.
const slashHelp = `Available commands:
  /help            Show this help
  /model [name]    Show or set the model (alias: haiku|sonnet|opus, or full ID)
  /allow <rule>    Auto-allow a tool rule this session, e.g. /allow Bash(go test:*)
  /deny <rule>     Auto-deny a tool rule this session
  /goal [text]     Set/show a standing goal re-stated each turn (/goal clear to stop)
  /memory [add …]  Show recalled memory, or add a note
  /mcp             List connected MCP servers and tools
  /stats           Show session stats (turns, tokens)
  /status          Show the current session settings
  /config          Show resolved provider/model/sandbox settings
  /agents          List available sub-agent types
  /context         Show working directory, git branch, and message count
  /cost            Show session token totals and an estimated cost
  /compact         Summarize and compact the conversation history now
  /add-dir <path>  Add a directory to the prompt context
  /plan [off]      Enter (or leave) read-only plan mode
  /doctor          Run environment diagnostics
  /diff [args]     Show git diff of the working tree
  /commit <msg>    Stage all changes and commit (asks first)
  /export          Export the conversation to a Markdown file
  /clear           Clear the screen and conversation history
  /quit, /exit     Exit Klaudia

Keys:
  Tab              Complete an @<path> reference from the working dir
  ↑ / ↓            Cycle through previous prompts
  Ctrl+C           Quit`

// handleSlash dispatches a slash command. Commands run locally and never reach
// the model.
func (m *Model) handleSlash(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "/help", "/?":
		m.appendLine(bannerStyle.Render(slashHelp + m.skillHelpLines()))
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
				cur = m.sess.ResolvedModel // show the concrete default, by name
			}
			if cur == "" {
				cur = "(default)"
			}
			m.appendLine(bannerStyle.Render("Model: " + cur))
		} else {
			m.sess.Model = args[0]
			m.appendLine(bannerStyle.Render("Model set to " + args[0] + " (applies to the next turn)."))
		}
	case "/goal":
		switch {
		case len(args) == 0:
			if m.sess.Goal == "" {
				m.appendLine(bannerStyle.Render("No standing goal set. /goal <text> to set one."))
			} else {
				m.appendLine(bannerStyle.Render("Goal: " + m.sess.Goal))
			}
		case strings.ToLower(args[0]) == "clear":
			m.sess.Goal = ""
			m.appendLine(bannerStyle.Render("Standing goal cleared."))
		default:
			m.sess.Goal = strings.Join(args, " ")
			m.appendLine(bannerStyle.Render("Standing goal set; it will be re-stated each turn:\n" + m.sess.Goal))
		}
	case "/memory":
		if m.sess.Memory == nil {
			m.appendLine(errStyle.Render("memory is not available"))
			break
		}
		if len(args) > 0 && strings.ToLower(args[0]) == "add" {
			note := strings.TrimSpace(strings.TrimPrefix(strings.Join(args, " "), args[0]))
			if err := m.sess.Memory.Add(note); err != nil {
				m.appendLine(errStyle.Render("memory: " + err.Error()))
			} else {
				m.appendLine(bannerStyle.Render("Saved to memory."))
			}
		} else {
			idx, _ := m.sess.Memory.Index()
			if strings.TrimSpace(idx) == "" {
				m.appendLine(bannerStyle.Render("No memory yet. /memory add <note> to save one."))
			} else {
				m.appendLine(bannerStyle.Render(strings.TrimSpace(idx)))
			}
		}
	case "/mcp":
		if len(m.sess.MCPSummary) == 0 {
			m.appendLine(bannerStyle.Render("No MCP servers connected. Configure them in .mcp.json or .klaudia/.mcp.json."))
		} else {
			m.appendLine(bannerStyle.Render("MCP servers:\n" + strings.Join(m.sess.MCPSummary, "\n")))
		}
	case "/stats":
		m.appendLine(bannerStyle.Render(fmt.Sprintf("Session: turns=%d  input_tokens=%d  output_tokens=%d",
			m.statTurns, m.statIn, m.statOut)))
	case "/allow", "/deny":
		if len(args) == 0 {
			m.appendLine(errStyle.Render("usage: " + cmd + " <rule>  e.g. " + cmd + " Bash(go test:*)"))
			break
		}
		rule, err := permission.ParseRule(strings.Join(args, " "))
		if err != nil {
			m.appendLine(errStyle.Render("invalid rule: " + err.Error()))
			break
		}
		if cmd == "/allow" {
			m.sessionAllow = append(m.sessionAllow, rule)
			m.appendLine(bannerStyle.Render("Will auto-allow " + permission.FormatRule(rule) + " this session."))
		} else {
			m.sessionDeny = append(m.sessionDeny, rule)
			m.appendLine(bannerStyle.Render("Will auto-deny " + permission.FormatRule(rule) + " this session."))
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
	case "/config":
		m.appendLine(bannerStyle.Render(m.renderConfig()))
	case "/agents":
		m.appendLine(bannerStyle.Render(m.renderAgents()))
	case "/context":
		m.appendLine(bannerStyle.Render(m.renderContext()))
	case "/cost":
		m.appendLine(bannerStyle.Render(m.renderCost()))
	case "/add-dir":
		if len(args) == 0 {
			if len(m.sess.ExtraDirs) == 0 {
				m.appendLine(bannerStyle.Render("No extra directories added. /add-dir <path> to add one."))
			} else {
				m.appendLine(bannerStyle.Render("Extra directories:\n  " + strings.Join(m.sess.ExtraDirs, "\n  ")))
			}
			break
		}
		dir := strings.Join(args, " ")
		m.sess.ExtraDirs = append(m.sess.ExtraDirs, dir)
		m.appendLine(bannerStyle.Render("Added directory (referenced in the prompt context next turn): " + dir))
	case "/compact":
		if m.sess.Compact == nil {
			m.appendLine(errStyle.Render("compaction is not available"))
			break
		}
		if len(m.history) == 0 {
			m.appendLine(bannerStyle.Render("Nothing to compact yet."))
			break
		}
		m.appendLine(bannerStyle.Render("Compacting conversation…"))
		m.state = stateRunning
		go func(hist []anthropic.BetaMessageParam) {
			newHist, summary, err := m.sess.Compact(m.ctx, hist)
			m.events <- compactDoneMsg{history: newHist, summary: summary, err: err}
		}(m.history)
		return m, m.spin.Tick
	case "/plan":
		if len(args) > 0 && strings.ToLower(args[0]) == "off" {
			m.sess.PermissionMode = string(permission.ModeDefault)
			m.appendLine(bannerStyle.Render("Left plan mode (default permissions)."))
		} else {
			m.sess.PermissionMode = string(permission.ModePlan)
			m.appendLine(bannerStyle.Render("Entered plan mode: read-only exploration; mutations are blocked. /plan off to leave."))
		}
	case "/doctor":
		if m.sess.Doctor == nil {
			m.appendLine(errStyle.Render("doctor is not available"))
		} else {
			m.appendLine(bannerStyle.Render(m.sess.Doctor()))
		}
	case "/diff":
		out, err := gitOutput(m.sess.CWD, append([]string{"diff"}, args...)...)
		switch {
		case err != nil:
			m.appendLine(errStyle.Render("git diff: " + err.Error()))
		case strings.TrimSpace(out) == "":
			m.appendLine(bannerStyle.Render("No changes."))
		default:
			m.appendLine(out)
		}
	case "/export":
		path, err := m.exportTranscript()
		if err != nil {
			m.appendLine(errStyle.Render("export: " + err.Error()))
		} else {
			m.appendLine(bannerStyle.Render("Exported transcript to " + path))
		}
	case "/commit":
		if len(args) == 0 {
			m.appendLine(errStyle.Render("usage: /commit <message>"))
			break
		}
		message := strings.Join(args, " ")
		status, err := gitOutput(m.sess.CWD, "status", "--short")
		if err != nil {
			m.appendLine(errStyle.Render("git: " + err.Error()))
			break
		}
		if strings.TrimSpace(status) == "" {
			m.appendLine(bannerStyle.Render("Nothing to commit (working tree clean)."))
			break
		}
		cwd := m.sess.CWD
		m.confirmAction = func() string {
			if _, err := gitOutput(cwd, "add", "-A"); err != nil {
				return "git add failed: " + err.Error()
			}
			if out, err := gitOutput(cwd, "commit", "-m", message); err != nil {
				return "git commit failed: " + strings.TrimSpace(out) + " " + err.Error()
			}
			return "Committed."
		}
		m.state = stateAwaitingConfirm
		m.appendLine(askStyle.Render("Stage all changes and commit?\n" + strings.TrimRight(status, "\n") + "\n(y)es / (n)o"))
	default:
		// A /<skill> matching a user-defined skill renders its body and submits it
		// as the turn prompt. Built-in commands above always win (a skill that
		// shadows one is unreachable here).
		if sk, ok := m.lookupSkill(strings.TrimPrefix(cmd, "/")); ok {
			rendered := sk.Render(strings.Join(args, " "))
			m.appendLine(bannerStyle.Render("Running skill /" + sk.Name))
			m.state = stateRunning
			m.startTurn(rendered)
			return m, m.spin.Tick
		}
		m.appendLine(errStyle.Render("Unknown command " + cmd + ". Try /help."))
	}
	return m, nil
}

// renderConfig shows the resolved provider/model/sandbox/permission settings.
func (m *Model) renderConfig() string {
	provider := m.sess.Provider
	if provider == "" {
		provider = "anthropic"
	}
	model := m.sess.ResolvedModel
	if model == "" {
		model = m.sess.Model
	}
	if model == "" {
		model = "(default)"
	}
	sandbox := m.sess.SandboxMode
	if sandbox == "" {
		sandbox = "local"
	}
	mode := m.sess.PermissionMode
	if mode == "" {
		mode = "default"
	}
	return fmt.Sprintf("Configuration:\n  provider=%s\n  model=%s\n  sandbox=%s\n  permission-mode=%s",
		provider, model, sandbox, mode)
}

// renderAgents lists the available sub-agent types (Agent tool subagent_type).
func (m *Model) renderAgents() string {
	if len(m.sess.Agents) == 0 {
		return "No sub-agent types available."
	}
	var b strings.Builder
	b.WriteString("Available sub-agent types (Agent tool):")
	for _, a := range m.sess.Agents {
		fmt.Fprintf(&b, "\n  %-16s %s", a.Name, a.Description)
	}
	return b.String()
}

// modelPrice is per-million-token USD pricing (input, output) for cost
// estimation. Matched by substring against the resolved model id; unknown
// models report tokens only.
type modelPrice struct{ in, out float64 }

var priceTable = map[string]modelPrice{
	"opus":   {15.0, 75.0},
	"sonnet": {3.0, 15.0},
	"haiku":  {0.80, 4.0},
	"gpt-5":  {1.25, 10.0},
}

// renderCost shows session token totals and a best-effort USD estimate.
func (m *Model) renderCost() string {
	model := m.sess.ResolvedModel
	if model == "" {
		model = m.sess.Model
	}
	base := fmt.Sprintf("Cost: turns=%d  input_tokens=%d  output_tokens=%d",
		m.statTurns, m.statIn, m.statOut)
	for key, p := range priceTable {
		if model != "" && strings.Contains(strings.ToLower(model), key) {
			usd := float64(m.statIn)/1e6*p.in + float64(m.statOut)/1e6*p.out
			return base + fmt.Sprintf("\n  estimated cost: $%.4f (model %s)", usd, model)
		}
	}
	return base + "\n  estimated cost: unknown (no pricing for model " + model + ")"
}

// renderContext shows the working directory, git branch, and model.
func (m *Model) renderContext() string {
	cwd := m.sess.CWD
	if cwd == "" {
		cwd = "(unknown)"
	}
	branch := m.sess.GitBranch
	if branch == "" {
		branch = "(none)"
	}
	return fmt.Sprintf("Context:\n  cwd=%s\n  git-branch=%s\n  messages=%d", cwd, branch, len(m.history))
}

// gitOutput runs `git <args>` in dir and returns combined output. A non-zero
// exit returns the output plus the error so callers can surface git's message.
func gitOutput(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	if dir != "" {
		c.Dir = dir
	}
	out, err := c.CombinedOutput()
	return string(out), err
}

// exportTranscript writes the conversation history to a timestamped Markdown
// file in the working directory and returns its path.
func (m *Model) exportTranscript() (string, error) {
	dir := m.sess.CWD
	if dir == "" {
		dir = "."
	}
	name := fmt.Sprintf("klaudia-export-%s.md", time.Now().Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(exportMarkdown(m.history)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// exportMarkdown renders the conversation history as Markdown (role headers +
// text blocks; tool calls/results are summarized).
func exportMarkdown(history []anthropic.BetaMessageParam) string {
	var b strings.Builder
	b.WriteString("# Klaudia conversation\n\n")
	b.WriteString("_Exported " + time.Now().Format(time.RFC3339) + "_\n")
	for _, msg := range history {
		fmt.Fprintf(&b, "\n## %s\n\n", capitalize(string(msg.Role)))
		for _, block := range msg.Content {
			switch {
			case block.OfText != nil:
				b.WriteString(block.OfText.Text + "\n")
			case block.OfToolUse != nil:
				fmt.Fprintf(&b, "_→ tool: %s_\n", block.OfToolUse.Name)
			case block.OfToolResult != nil:
				b.WriteString("_← tool result_\n")
			}
		}
	}
	return b.String()
}

// completeAtPath completes the trailing "@<partial>" token in the input against
// files under the working directory. A unique match is filled in; multiple
// matches fill the common prefix and list candidates as a banner.
func (m *Model) completeAtPath() {
	value := m.input.Value()
	at := strings.LastIndex(value, "@")
	if at < 0 {
		return
	}
	partial := value[at+1:]
	if strings.ContainsAny(partial, " \t") {
		return // the @token has already been closed by whitespace
	}
	cands := matchPaths(m.sess.CWD, partial)
	if len(cands) == 0 {
		return
	}
	completion := cands[0]
	if len(cands) > 1 {
		completion = commonPrefix(cands)
		// Show up to 12 candidates so the user can keep typing.
		show := cands
		if len(show) > 12 {
			show = show[:12]
		}
		m.appendLine(bannerStyle.Render("candidates: " + strings.Join(show, "  ")))
	}
	m.input.SetValue(value[:at+1] + completion)
	m.input.CursorEnd()
}

// matchPaths returns working-dir-relative file paths that start with partial
// (case-insensitive), sorted, capped. A blank partial lists top entries.
func matchPaths(cwd, partial string) []string {
	root := cwd
	if root == "" {
		root = "."
	}
	files, err := search.Glob(search.GlobOptions{Root: root})
	if err != nil {
		return nil
	}
	lower := strings.ToLower(partial)
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		rel, err := filepath.Rel(root, f)
		if err != nil {
			rel = f
		}
		if partial == "" || strings.HasPrefix(strings.ToLower(rel), lower) {
			if !seen[rel] {
				seen[rel] = true
				out = append(out, rel)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

// commonPrefix returns the longest shared leading substring of the inputs.
func commonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
			if p == "" {
				return ""
			}
		}
	}
	return p
}

// maxInputHistory caps the remembered prompts (ring buffer).
const maxInputHistory = 200

// pushHistory appends a submitted prompt (dropping an immediate duplicate) and
// resets the navigation cursor to "not navigating".
func (m *Model) pushHistory(prompt string) {
	if n := len(m.inputHistory); n == 0 || m.inputHistory[n-1] != prompt {
		m.inputHistory = append(m.inputHistory, prompt)
		if len(m.inputHistory) > maxInputHistory {
			m.inputHistory = m.inputHistory[len(m.inputHistory)-maxInputHistory:]
		}
	}
	m.histPos = len(m.inputHistory)
	m.histDraft = ""
}

// navigateHistory moves through prior prompts: up = older, down = newer. The
// in-progress line is stashed on first up and restored past the newest entry.
func (m *Model) navigateHistory(up bool) {
	if len(m.inputHistory) == 0 {
		return
	}
	if up {
		if m.histPos == len(m.inputHistory) {
			m.histDraft = m.input.Value() // stash the fresh line
		}
		if m.histPos > 0 {
			m.histPos--
		}
	} else {
		if m.histPos < len(m.inputHistory) {
			m.histPos++
		}
	}
	if m.histPos >= len(m.inputHistory) {
		m.input.SetValue(m.histDraft)
	} else {
		m.input.SetValue(m.inputHistory[m.histPos])
	}
	m.input.CursorEnd()
}

// capitalize upper-cases the first rune (role headers in the export).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// lookupSkill finds a loaded skill by name (case-sensitive).
func (m *Model) lookupSkill(name string) (SkillCommand, bool) {
	if m.sess == nil {
		return SkillCommand{}, false
	}
	for _, sk := range m.sess.Skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return SkillCommand{}, false
}

// skillHelpLines renders the user-defined skills section of /help (empty when
// none are loaded).
func (m *Model) skillHelpLines() string {
	if m.sess == nil || len(m.sess.Skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nSkills:")
	for _, sk := range m.sess.Skills {
		desc := sk.Description
		if desc == "" {
			desc = "(user-defined skill)"
		}
		fmt.Fprintf(&b, "\n  /%-14s %s", sk.Name, desc)
	}
	return b.String()
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
// A standing /goal is re-stated to the model each turn (Ralph-style).
func (m *Model) startTurn(prompt string) {
	if m.sess != nil && m.sess.Goal != "" {
		prompt = fmt.Sprintf("Standing goal for this session: %s\n\nCurrent instruction: %s", m.sess.Goal, prompt)
	}
	approver := &uiApprover{events: m.events}
	asker := &uiAsker{events: m.events}
	planner := &uiPlanner{events: m.events}
	emit := func(ev agent.Event) { m.events <- eventMsg{ev} }
	go func() {
		res, err := m.run(m.ctx, prompt, m.history, approver, asker, planner, emit)
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
		bottom = askStyle.Render(fmt.Sprintf("Allow %s? (y)es once / (a)lways / (n)o", m.pendingReq.ToolName))
	case stateAwaitingAnswer:
		bottom = askStyle.Render(fmt.Sprintf("Choose 1-%d", len(m.askOptions)))
	case stateAwaitingPlan:
		bottom = askStyle.Render("Approve plan? (y)es / (n)o")
	case stateAwaitingConfirm:
		bottom = askStyle.Render("Confirm? (y)es / (n)o")
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

// uiAsker implements tools.Asker by prompting the user in the UI and blocking
// for their choice.
type uiAsker struct {
	events chan tea.Msg
}

func (a *uiAsker) Ask(ctx context.Context, question string, options []tools.AskOption) (string, error) {
	reply := make(chan string, 1)
	a.events <- askMsg{question: question, options: options, reply: reply}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case choice := <-reply:
		return choice, nil
	}
}

// uiPlanner implements tools.Planner by showing the plan and blocking for the
// user's approval.
type uiPlanner struct {
	events chan tea.Msg
}

func (p *uiPlanner) ExitPlan(ctx context.Context, plan string) (bool, error) {
	reply := make(chan bool, 1)
	p.events <- planMsg{plan: plan, reply: reply}
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case ok := <-reply:
		return ok, nil
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
