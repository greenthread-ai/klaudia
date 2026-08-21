// Package tui is the interactive terminal frontend, built on Bubble Tea. It is
// a peer of the headless and stream-json frontends: it drives the same agent
// core (RunFunc), consumes the Emitter event stream, and resolves permission
// asks via an in-UI prompt (implementing agent.Approver).
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/stopwatch"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/compaction"
	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/goal"
	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/internal/native/search"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// RunFunc drives one user turn against the agent core, threading conversation
// history and using the supplied approver, asker, and emitter.
type RunFunc func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, approver agent.Approver, asker tools.Asker, planner tools.Planner, emit agent.Emitter) (agent.Result, error)

// Session is mutable state shared between the TUI and the RunFunc closure, so
// slash commands like /model can change settings for subsequent turns. The
// RunFunc should read these fields fresh on each call.
type Session struct {
	SessionID      string         // transcript/session id used by --resume
	Model          string         // model alias or full ID ("" = default)
	ResolvedModel  string         // concrete model id for display
	PermissionMode string         // live mode (ExitPlanMode flips it out of "plan")
	Memory         memory.Store   // backs /memory; never nil — set to memory.Disabled() when unavailable
	Goal           string         // standing goal re-injected each turn (Ralph-style)
	Theme          string         // markdown render theme ("" = dark)
	Skills         []SkillCommand // user-defined skills dispatched as /<name>

	// Render-only context for /config and /context (set once at startup).
	Provider    string      // resolved provider ("anthropic" | "openai" | …)
	SandboxMode string      // resolved sandbox mode ("local" | "os" | "container")
	CWD         string      // working directory
	GitBranch   string      // current git branch (may be "")
	Agents      []AgentInfo // built-in sub-agent types, for /agents
	ExtraDirs   []string    // additional working dirs added via /add-dir
	// ContextWindow is the input-token limit /stats reports against; resolved
	// at startup via api.ContextWindow (config override > model default > 0).
	// Zero means "unknown"; /stats omits the usage ratio in that case.
	ContextWindow       int
	ContextWindowSource string

	// Compact, if set, runs a model-based compaction of the given history and
	// returns the replacement history plus the summary. Backs /compact.
	Compact CompactFunc
	// Doctor, if set, returns a rendered environment diagnostic. Backs /doctor.
	Doctor func() string
	// MCP, if set, lets /mcp inspect and reconnect/disconnect servers. May be nil.
	MCP MCPController
}

// MCPController lets the TUI manage MCP servers without owning the manager.
type MCPController interface {
	Servers() []MCPServerInfo
	Reconnect(name string) error
	Disconnect(name string) error
}

// MCPServerInfo is one MCP server's status for the /mcp view.
type MCPServerInfo struct {
	Name      string
	Connected bool
	Tools     int
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
	stateAwaitingChoice
)

// choiceItem is one option in a local settings picker (e.g. /mode). apply runs
// when the user selects it and returns a confirmation line.
type choiceItem struct {
	label string
	apply func() string
}

// --- messages delivered from the agent goroutine ---

type (
	eventMsg      struct{ ev agent.Event }
	permissionMsg struct {
		req   agent.ApprovalRequest
		reply chan permission.Decision
	}
)

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

// Chrome styles. The accent-bearing ones (logo/heading/suggestion/prompt) are
// re-derived from the active theme by applyChromeTheme; errors stay red and
// banner/tool/hint stay neutral so body text and warnings read clearly on any
// theme. Initialised to the default palette here.
var (
	userStyle    = lipgloss.NewStyle()
	toolStyle    = lipgloss.NewStyle()
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	askStyle     = lipgloss.NewStyle()
	bannerStyle  = lipgloss.NewStyle().Faint(true)
	logoStyle    = lipgloss.NewStyle()
	hintStyle    = lipgloss.NewStyle().Faint(true).Italic(true)
	suggestStyle = lipgloss.NewStyle()
)

func init() { applyChromeTheme(defaultChromePalette) }

// applyChromeTheme recolours the accent chrome styles from a theme palette so
// the banner, pickers, prompts, and type-ahead follow /theme (not just the
// rendered Markdown). Called at startup and on every theme change.
func applyChromeTheme(p themePalette) {
	accent := lipgloss.Color(p.accent)
	accent2 := lipgloss.Color(p.accent2)
	muted := lipgloss.Color(p.muted)
	logoStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	askStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	suggestStyle = lipgloss.NewStyle().Foreground(accent2)
	userStyle = lipgloss.NewStyle().Bold(true).Foreground(accent2)
	toolStyle = lipgloss.NewStyle().Foreground(muted)
	hintStyle = lipgloss.NewStyle().Faint(true).Italic(true).Foreground(muted)
}

// intro is the welcoming banner shown at startup. The model name/branch/session
// id are filled in by the caller.
// intro renders the startup banner. The session id is intentionally absent:
// interactive runs auto-resume the most recent project session, so reciting it
// (and a manual `--resume` command) on every launch is noise; /status surfaces
// it on demand.
func intro(model, branch, tagline string) string {
	logo := logoStyle.Render("✦ Klaudia")
	tag := bannerStyle.Render(" " + tagline)
	var meta string
	if model != "" {
		meta = "\n" + bannerStyle.Render("  model: "+model)
	}
	if branch != "" {
		meta += bannerStyle.Render("   ⎇ " + branch)
	}
	tip := hintStyle.Render("\n  Type a prompt and press Enter · / for commands · @ to reference a file · Esc to interrupt · Ctrl+C to quit")
	return logo + tag + meta + tip + "\n"
}

// Model is the Bubble Tea model for the interactive REPL.
type Model struct {
	run    RunFunc
	events chan tea.Msg
	ctx    context.Context

	vp     viewport.Model
	input  textarea.Model
	spin   spinner.Model
	state  uiState
	ready  bool
	width  int
	height int
	// follow keeps the viewport pinned to the bottom as new output arrives. It
	// is true until the user scrolls up to read history, and becomes true again
	// when they scroll back to the bottom. Programmatic appends and viewport
	// resizes honor it rather than re-deriving "are we at the bottom?", which is
	// unreliable across height changes (e.g. the "working…" line shrinking vp).
	follow bool

	transcript strings.Builder // rendered scrollback
	rawBlocks  []transcriptBlock
	history    []anthropic.BetaMessageParam
	pending    chan permission.Decision
	pendingReq agent.ApprovalRequest
	sess       *Session
	// Session-scoped allow/deny rules added via "allow always" or /allow,/deny.
	// Accessed only from the UI goroutine (Update) to avoid races.
	sessionAllow []permission.Rule
	sessionDeny  []permission.Rule
	// Goal/loop state (the "/goal" feature). goalSetting frames turns as
	// spec-authoring. loopRemaining>0 means the "/goal run" Ralph loop is active:
	// each finished iteration starts the next (via the doneMsg hook) until the
	// goal completes, the count hits 0, or the user stops it.
	goalSetting    bool
	loopRemaining  int
	loopTotal      int
	loopSpecPath   string
	loopWrapUp     bool   // the next loop turn is the end-of-run summary
	loopStubFixing bool   // the next loop turn is the up-front Progress-stub repair
	loopVerifying  bool   // the next loop turn is the final-review verification
	loopBranch     string // the goal branch the loop's work lands on
	loopBaseBranch string // the branch the loop started from (merge target)
	// quitArmed is set by a Ctrl+C press that had nothing left to cancel (see
	// onCtrlC). While armed the status bar says so, and an immediately repeated
	// Ctrl+C quits; any other key disarms it. This is what stops a reflexive
	// "stop the running thing" Ctrl+C from destroying the session.
	quitArmed bool
	// cancelling is set when the user pressed Esc but the agent goroutine
	// hasn't returned yet (e.g. blocked in SDK retry backoff or a tool that's
	// slow to honour ctx). The bottom view swaps "working…" for "cancelling…"
	// so the user gets immediate feedback that Esc registered, and knows Ctrl+C
	// is the next escalation. Cleared on doneMsg.
	cancelling bool
	// Cumulative session stats for /stats. Updated live via "usage" events from
	// the agent loop (per inner LLM call), then reconciled against the
	// authoritative Result fields when doneMsg arrives — so a long /goal
	// iteration shows progress in the status bar mid-turn rather than staying
	// at zero until the whole iteration concludes.
	statTurns int
	statIn    int64
	statOut   int64
	// Per-turn tally of what we already counted via live "usage" events. Reset
	// at startTurn and subtracted from the final Result at doneMsg so a dropped
	// usage event still settles correctly without double-counting.
	turnLiveTurns int
	turnLiveIn    int64
	turnLiveOut   int64
	// Phase tracking for the spinner row. phase reflects the most recent
	// meaningful event ("streaming", "running <Tool>", "compacting",
	// "thinking"); lastEventAt is bumped on every event the renderer sees,
	// used by bottomView to surface a "quiet for…" suffix when an API call
	// sits silent. activeTool* track the most recent in-flight tool_use so
	// tool_result can compute elapsed and append a duration to the result.
	phase           string
	lastEventAt     time.Time
	activeToolName  string
	activeToolStart time.Time
	// residentTokens is the last estimated input-context size, refreshed at
	// doneMsg after history is reconciled. The status bar reads this as a
	// `· ctx N%` indicator against sess.ContextWindow so users see context
	// pressure without having to type /stats. Computed once per turn end
	// rather than per render — compaction.EstimateTokens walks the whole
	// history and isn't cheap on long sessions.
	residentTokens int
	lastResult     string // full content of the most recent tool result (for /last)
	queued         string // a message typed while the model is working (sent on completion)
	// Pending AskUserQuestion.
	askReply    chan string
	askOptions  []tools.AskOption
	askQuestion string
	// Pending ExitPlanMode approval.
	planReply chan bool
	// Pending /commit-style confirmation: run on "y", returns a result line.
	confirmAction func() string
	// Pending local settings picker (e.g. /mode): numbered choices.
	choiceItems  []choiceItem
	choicePrompt string
	// Input history: submitted prompts (newest last), navigated with Up/Down.
	// histPos == len(inputHistory) means "not navigating" (editing a fresh line).
	inputHistory []string
	histPos      int
	histDraft    string // the in-progress line stashed when navigating up
	// Elapsed-run stopwatch and per-turn cancel (Esc interrupts the model).
	sw         stopwatch.Model
	turnCancel context.CancelFunc
	// streamBuf holds the in-progress assistant message (shown raw as it
	// streams); on completion it's flushed through glamour into the transcript.
	streamBuf strings.Builder
	glam      *glamour.TermRenderer
	glamWidth int
	// Intro banner inputs, so it can be regenerated (recoloured) on theme change.
	// introTagline is chosen once so it stays stable across regenerations.
	introModel, introBranch, introTagline string
	hasIntro                              bool
}

type transcriptBlock struct {
	text     string
	markdown bool
}

// New builds the model. ctx cancels in-flight turns when the program exits.
// history seeds the conversation when resuming a session (may be nil). sess is
// shared mutable settings (may be nil).
func New(ctx context.Context, run RunFunc, history []anthropic.BetaMessageParam, sess *Session) *Model {
	if sess == nil {
		sess = &Session{}
	}
	in := newPromptInput()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := &Model{
		run:     run,
		events:  make(chan tea.Msg, 256),
		ctx:     ctx,
		input:   in,
		spin:    sp,
		sw:      stopwatch.NewWithInterval(100 * time.Millisecond),
		state:   stateIdle,
		history: history,
		sess:    sess,
		follow:  true,
	}
	// Colour the chrome for the session's theme before drawing the banner.
	applyChromeTheme(chromePaletteFor(m.currentThemeID()))
	model, branch := "", ""
	if sess != nil {
		model, branch = sess.displayModel(), sess.GitBranch
	}
	m.introModel, m.introBranch = model, branch
	m.introTagline, m.hasIntro = randomTagline(), true
	introText := intro(model, branch, m.introTagline)
	m.transcript.WriteString(introText)
	m.rawBlocks = append(m.rawBlocks, transcriptBlock{text: introText})
	return m
}

func newPromptInput() textarea.Model {
	in := textarea.New()
	in.Placeholder = "Ask Klaudia… (Enter to send, Ctrl+J for newline, Ctrl+C to quit)"
	in.Prompt = ""
	in.ShowLineNumbers = false
	in.EndOfBufferCharacter = ' '
	in.CharLimit = 0
	in.MaxHeight = 6
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j"), key.WithHelp("ctrl+j", "newline"))
	in.Focus()
	return in
}

// setState changes the UI state and re-syncs the layout (the running and idle
// states reserve different numbers of bottom rows).
func (m *Model) setState(s uiState) {
	m.state = s
	m.syncInputHeight()
}

func (m *Model) inputHeight() int {
	// The input is shown (and editable) when idle and while the model works
	// (for queueing a follow-up); other states show a one-line prompt.
	if m.state != stateIdle && m.state != stateRunning {
		return 1
	}
	// Count wrapped display rows, not logical lines: a single long line that
	// the textarea soft-wraps must grow the box rather than scroll within one
	// row. (LineCount counts logical lines only.)
	h := wrappedRowCount(m.input.Value(), m.input.Width())
	if h < 1 {
		return 1
	}
	if h > m.input.MaxHeight {
		return m.input.MaxHeight
	}
	return h
}

// syncInputHeight (re)sizes the viewport to exactly fill the space above the
// bottom area, measured from the rendered bottom block so the reservation can
// never drift from what View actually draws. (View = viewport + "\n" + bottom,
// so viewportHeight = terminalHeight - bottomHeight.)
func (m *Model) syncInputHeight() {
	if !m.ready {
		return
	}
	m.input.SetHeight(m.inputHeight())
	h := m.height - lipgloss.Height(m.bottomView())
	if h < 1 {
		h = 1
	}
	m.vp.Height = h
	m.syncViewport()
}

// displayModel returns the model name to show in the intro/status.
func (s *Session) displayModel() string {
	if s.Model != "" {
		return s.Model
	}
	return s.ResolvedModel
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.waitForEvent())
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

	case tea.MouseMsg:
		return m.onMouse(msg)

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
		m.setState(stateAwaitingPermission)
		m.pending = msg.reply
		m.pendingReq = msg.req
		m.appendLine(askStyle.Render("Permission required: " + m.permissionSummary(msg.req)))
		if detail := permissionDetail(msg.req); detail != "" {
			m.appendLine(toolStyle.Render("  " + detail))
		}
		// The actionable prompt lives only in the persistent bottom view (see
		// bottomView/stateAwaitingPermission) — don't duplicate it in scrollback.
		return m, m.waitForEvent()

	case askMsg:
		m.setState(stateAwaitingAnswer)
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
		m.setState(stateAwaitingPlan)
		m.planReply = msg.reply
		m.appendLine(askStyle.Render("Proposed plan:"))
		m.appendMarkdown(msg.plan)
		// The y/n prompt lives only in the persistent bottom view (see
		// bottomView/stateAwaitingPlan); don't also write it to scrollback or
		// the user sees the same prompt twice.
		return m, m.waitForEvent()

	case doneMsg:
		elapsed := m.sw.Elapsed()
		stopSW := m.sw.Stop()
		m.turnCancel = nil
		m.cancelling = false // the goroutine returned; we're past the cancel window
		m.flushAssistant()   // prettify the final answer through glamour
		switch {
		case errors.Is(msg.err, context.Canceled):
			m.appendLine(toolStyle.Render(fmt.Sprintf("  ⊘ interrupted after %s", fmtDuration(elapsed))))
		case msg.err != nil:
			m.appendLine(errStyle.Render("error: " + api.FriendlyError(msg.err)))
		default:
			m.appendLine(bannerStyle.Render("  ✓ done in " + fmtDuration(elapsed) + throughput(msg.res.OutputTokens, elapsed)))
		}
		if msg.res.Messages != nil {
			m.history = msg.res.Messages
		}
		// Refresh the cached resident-tokens estimate for the status bar's
		// `· ctx N%` indicator. Doing it here (after history is reconciled)
		// avoids walking history on every render, which gets expensive on
		// long sessions — the indicator updates once per turn, which is
		// the natural rhythm for context-pressure feedback anyway.
		m.residentTokens = compaction.EstimateTokens(m.history)
		// Reconcile live usage with the authoritative Result. If every "usage"
		// event made it through, these deltas are zero (no double-count); if
		// some were dropped due to channel pressure, this catches the gap up
		// to the final accurate totals.
		m.statTurns += msg.res.NumTurns - m.turnLiveTurns
		m.statIn += msg.res.InputTokens - m.turnLiveIn
		m.statOut += msg.res.OutputTokens - m.turnLiveOut
		m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut = 0, 0, 0
		// Clear phase state too so a queued-message follow-up turn starts
		// fresh — a stale "running Bash" or "quiet for 90s" would otherwise
		// briefly flash on the next turn's first render before its own
		// startTurn reset.
		m.phase = ""
		m.lastEventAt = time.Time{}
		m.activeToolName = ""
		m.activeToolStart = time.Time{}
		// Drop any approval/ask/plan channels a turn was interrupted mid-prompt.
		m.pending, m.askReply, m.planReply = nil, nil, nil
		// Goal loop: run the next iteration (or the wrap-up turn) unless the goal
		// is complete, the turn errored/was interrupted, or we've fully stopped.
		if m.loopRemaining > 0 || m.loopWrapUp {
			if next := m.loopNext(msg.res, msg.err); next != "" {
				m.setState(stateRunning)
				return m, tea.Batch(m.waitForEvent(), m.startTurn(next), stopSW)
			}
		}
		// A message queued while this turn ran is sent now as the next turn.
		if q := strings.TrimSpace(m.queued); q != "" {
			m.queued = ""
			m.pushHistory(q)
			m.appendLine(userStyle.Render("› ") + q)
			m.setState(stateRunning)
			return m, tea.Batch(m.waitForEvent(), m.startTurn(q), stopSW)
		}
		m.setState(stateIdle)
		m.input.Focus()
		return m, tea.Batch(textarea.Blink, m.waitForEvent(), stopSW)

	case compactDoneMsg:
		if msg.err != nil {
			m.appendLine(errStyle.Render("compact: " + api.FriendlyError(msg.err)))
		} else {
			m.history = msg.history
			m.appendLine(bannerStyle.Render("Compacted conversation. Summary:\n" + strings.TrimSpace(msg.summary)))
		}
		m.setState(stateIdle)
		m.input.Focus()
		return m, tea.Batch(textarea.Blink, m.waitForEvent())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case stopwatch.TickMsg, stopwatch.StartStopMsg, stopwatch.ResetMsg:
		var cmd tea.Cmd
		m.sw, cmd = m.sw.Update(msg)
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInputHeight()
	return m, cmd
}

func (m *Model) onMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	// A wheel scroll is a follow-intent signal: stop following when the user
	// scrolls up, resume once they're back at the bottom.
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.follow = false
	case tea.MouseButtonWheelDown:
		m.follow = m.vp.AtBottom()
	}
	return m, cmd
}

// interruptTurn cancels the in-flight turn. The cancelled context unblocks the
// agent goroutine (including any approval/question/plan prompt it is parked on,
// since those select on ctx.Done()), which then sends doneMsg. Shared by Esc and
// the first press of Ctrl+C so the two can never drift apart.
func (m *Model) interruptTurn() {
	m.turnCancel()
	m.turnCancel = nil
	m.loopRemaining, m.loopWrapUp, m.loopStubFixing, m.loopVerifying = 0, false, false, false // also halts a goal loop
	m.cancelling = true
	m.appendLine(toolStyle.Render("  ⊘ interrupting… (Ctrl+C again to force quit)"))
}

// onCtrlC implements two-press Ctrl+C. A terminal user's reflex is that Ctrl+C
// stops the thing that is running, not that it destroys the session — so the
// first press always does the smallest useful thing and only an immediately
// repeated press quits. Resolution order:
//
//  1. armed by a previous press → quit;
//  2. a turn is in flight → interrupt it (and arm, so a wedged turn can still
//     be escaped — this is what the "Ctrl+C again to force quit" hint promises);
//  3. a local y/n or picker prompt is open → cancel it;
//  4. the draft line is non-empty → clear it (readline's Ctrl+C);
//  5. otherwise → arm, and let the status bar say so.
//
// Note that the permission/ask/plan prompts are NOT handled in step 3: those
// only exist while a turn is running, so step 2 catches them and cancelling the
// turn context is what releases the blocked agent goroutine.
func (m *Model) onCtrlC() (tea.Model, tea.Cmd) {
	if m.quitArmed {
		return m, tea.Quit
	}
	if m.turnCancel != nil {
		m.interruptTurn()
		m.quitArmed = true
		return m, nil
	}
	switch m.state {
	case stateAwaitingConfirm:
		m.confirmAction = nil
		m.setState(stateIdle)
		m.appendLine(toolStyle.Render("  → cancelled"))
		return m, nil
	case stateAwaitingChoice:
		m.choiceItems, m.choicePrompt = nil, ""
		m.setState(stateIdle)
		m.appendLine(toolStyle.Render("  → cancelled"))
		return m, nil
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		m.input.Reset()
		m.syncInputHeight()
		return m, nil
	}
	m.quitArmed = true
	return m, nil
}

func (m *Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Any key other than Ctrl+C disarms a pending "press again to quit".
	if msg.Type != tea.KeyCtrlC {
		m.quitArmed = false
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		return m.onCtrlC()
	case tea.KeyEsc:
		// Interrupt the in-flight turn (and any pending approval/question it is
		// blocked on). The cancelled context unblocks the agent goroutine, which
		// then sends doneMsg.
		if m.turnCancel != nil {
			m.interruptTurn()
			return m, nil
		}
	}

	// Scrollback works in any state and never reaches the text input. (Up/Down
	// are reserved for input history, so paging uses PgUp/PgDn. Ctrl+U and
	// Ctrl+D are deliberately NOT bound here — they belong to the input as
	// readline kill-line and EOF.)
	switch msg.Type {
	case tea.KeyPgUp:
		m.vp.PageUp()
		m.follow = false
		return m, nil
	case tea.KeyPgDown:
		m.vp.PageDown()
		m.follow = m.vp.AtBottom()
		return m, nil
	}

	// While the model works, the input stays editable. A slash command runs
	// immediately (display/config ones apply now; the few that mutate history or
	// start a turn refuse until you interrupt). Plain text is queued as a
	// follow-up: Enter queues it; Enter again (empty) interrupts and sends it; ↑
	// edits it.
	if m.state == stateRunning {
		switch msg.Type {
		case tea.KeyEnter:
			text := strings.TrimSpace(m.input.Value())
			if strings.HasPrefix(text, "/") {
				m.input.Reset()
				m.pushHistory(text)
				m.appendLine(userStyle.Render("› ") + text)
				m.syncInputHeight()
				return m.handleSlash(text)
			}
			if text != "" {
				m.queued = text
				m.input.Reset()
				m.syncInputHeight()
				return m, nil
			}
			if m.queued != "" && m.turnCancel != nil {
				m.turnCancel()
				m.turnCancel = nil
				m.cancelling = true // bottom view swaps to "cancelling…" so user sees the cancel registered
				m.appendLine(toolStyle.Render("  ⊘ interrupting to send your queued message…"))
			}
			return m, nil
		case tea.KeyUp:
			if m.queued != "" { // recall the queued message to edit it
				m.input.SetValue(m.queued)
				m.queued = ""
				m.input.CursorEnd()
				m.syncInputHeight()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncInputHeight()
		return m, cmd
	}

	if m.state == stateAwaitingPlan {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.planReply <- true
			m.planReply = nil
			m.sess.PermissionMode = string(permission.ModeAcceptEdits) // leave plan mode
			m.appendLine(toolStyle.Render("  → approved; plan mode off (acceptEdits)"))
			m.setState(stateRunning)
		case "n":
			m.planReply <- false
			m.planReply = nil
			m.appendLine(toolStyle.Render("  → not approved; staying in plan mode"))
			m.setState(stateRunning)
		}
		return m, nil
	}

	if m.state == stateAwaitingConfirm {
		switch strings.ToLower(msg.String()) {
		case "y":
			action := m.confirmAction
			m.confirmAction = nil
			m.setState(stateIdle)
			if action != nil {
				m.appendLine(bannerStyle.Render(action()))
			}
		case "n":
			m.confirmAction = nil
			m.setState(stateIdle)
			m.appendLine(toolStyle.Render("  → cancelled"))
		}
		return m, nil
	}

	if m.state == stateAwaitingChoice {
		if msg.Type == tea.KeyEsc {
			m.choiceItems, m.choicePrompt = nil, ""
			m.setState(stateIdle)
			m.appendLine(toolStyle.Render("  → cancelled"))
			return m, nil
		}
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if n := int(s[0] - '0'); n <= len(m.choiceItems) {
				item := m.choiceItems[n-1]
				m.choiceItems, m.choicePrompt = nil, ""
				m.setState(stateIdle)
				m.appendLine(bannerStyle.Render("  → " + item.apply()))
			}
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
				m.setState(stateRunning)
			}
		}
		return m, nil
	}

	if m.state == stateAwaitingPermission {
		switch strings.ToLower(msg.String()) {
		case "y":
			m.answer(permission.Decision{Behavior: permission.Allow})
		case "a":
			rule := permission.Rule{Tool: m.pendingReq.ToolName, Specifier: m.pendingReq.Specifier}
			m.rememberPermission("allow", rule)
			m.answer(permission.Decision{Behavior: permission.Allow})
		case "n":
			m.answer(permission.Decision{Behavior: permission.Deny, Message: "denied by user"})
		}
		return m, nil
	}

	// Tab completion on an idle line: slash command when the line is a "/token",
	// otherwise an @<path> reference.
	if m.state == stateIdle && msg.Type == tea.KeyTab {
		if strings.HasPrefix(m.input.Value(), "/") && !strings.ContainsAny(m.input.Value(), " \t") {
			m.completeSlash()
		} else {
			m.completeAtPath()
		}
		return m, nil
	}

	// Input history navigation (only on a fresh idle line).
	if m.state == stateIdle && m.input.LineCount() <= 1 && (msg.Type == tea.KeyUp || msg.Type == tea.KeyDown) {
		m.navigateHistory(msg.Type == tea.KeyUp)
		return m, nil
	}

	if msg.Type == tea.KeyCtrlJ && m.state == stateIdle {
		m.input.InsertString("\n")
		m.syncInputHeight()
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

		m.setState(stateRunning)
		// Do NOT arm another waitForEvent here: exactly one channel reader must
		// be outstanding (Init armed it; each channel-event handler re-arms it).
		// A second reader would race and deliver streamed deltas out of order.
		// startTurn returns only spinner/stopwatch ticks (separate cmd loops).
		return m, m.startTurn(prompt)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInputHeight()
	return m, cmd
}

// cmdInfo describes one slash command — the single source of truth for both
// /help and the type-ahead suggestions, so the two can never drift.
type cmdInfo struct{ name, args, desc string }

var commandList = []cmdInfo{
	{"/help", "", "Show this help"},
	{"/model", "[name]", "Show or set the model (alias: haiku|sonnet|opus, or full ID)"},
	{"/theme", "[name]", "Change Markdown render theme (no arg = picker)"},
	{"/mode", "[name]", "Change how Klaudia asks permission (no arg = picker)"},
	{"/allow", "<rule>", "Auto-allow a tool rule this session, e.g. /allow Bash(go test:*)"},
	{"/deny", "<rule>", "Auto-deny a tool rule this session"},
	{"/goal", "[run N|stop|text]", "No arg: goal-setting (draft/load a spec). run [N]: iterate to the goal. stop: halt. text: standing reminder"},
	{"/memory", "[add|recent|stale|tag|promote|supersede]", "Show / audit / curate memory; no args views the index"},
	{"/mcp", "", "List MCP servers; reconnect or disconnect them"},
	{"/stats", "", "Show session stats (turns, tokens)"},
	{"/status", "", "Show the current session settings"},
	{"/config", "", "Show resolved provider/model/sandbox settings"},
	{"/agents", "", "List available sub-agent types"},
	{"/context", "", "Show working directory, git branch, and message count"},
	{"/compact", "", "Summarize and compact the conversation history now"},
	{"/add-dir", "<path>", "Add a directory to the prompt context"},
	{"/plan", "[off]", "Enter (or leave) read-only plan mode"},
	{"/doctor", "", "Run environment diagnostics"},
	{"/diff", "[args]", "Show git diff of the working tree"},
	{"/commit", "<msg>", "Stage all changes and commit (asks first)"},
	{"/export", "", "Export the conversation to a Markdown file"},
	{"/last", "", "Reprint the most recent tool output in full"},
	{"/clear", "", "Clear the screen and conversation history"},
	{"/quit", "", "Exit Klaudia (alias /exit)"},
}

// keyHints documents the non-command key bindings shown in /help.
const keyHints = `Keys:
  /                Type a slash to see matching commands
  Tab              Complete a /command or an @<path> reference
  ↑ / ↓            Cycle through previous prompts
  PgUp / PgDn      Scroll the conversation history
  Esc              Interrupt the model mid-turn
  Ctrl+C           Interrupt, or clear the line — press twice to quit`

// slashHelp renders the command reference from commandList + keyHints.
func slashHelp() string {
	var b strings.Builder
	b.WriteString("Available commands:")
	for _, c := range commandList {
		left := c.name
		if c.args != "" {
			left += " " + c.args
		}
		fmt.Fprintf(&b, "\n  %-16s %s", left, c.desc)
	}
	b.WriteString("\n\n")
	b.WriteString(keyHints)
	return b.String()
}

// builtinCommands returns the canonical command names for type-ahead.
func builtinCommands() []string {
	out := make([]string, len(commandList))
	for i, c := range commandList {
		out[i] = c.name
	}
	return out
}

// slashSuggestions returns commands (built-ins + skills) that start with the
// current "/partial" token, when the user is typing a command on a fresh line.
func (m *Model) slashSuggestions() []string {
	value := m.input.Value()
	if !strings.HasPrefix(value, "/") || strings.ContainsAny(value, " \t") {
		return nil
	}
	var out []string
	for _, c := range builtinCommands() {
		if strings.HasPrefix(c, value) {
			out = append(out, c)
		}
	}
	if m.sess != nil {
		for _, sk := range m.sess.Skills {
			if name := "/" + sk.Name; strings.HasPrefix(name, value) {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// slashSuggestionLine renders up to 8 type-ahead command suggestions, or "" when
// there is nothing to suggest (or the only match is exactly what's typed).
func (m *Model) slashSuggestionLine() string {
	sug := m.slashSuggestions()
	if len(sug) == 0 || (len(sug) == 1 && sug[0] == m.input.Value()) {
		return ""
	}
	if len(sug) > 8 {
		sug = append(sug[:8], "…")
	}
	return suggestStyle.Render(strings.Join(sug, "  ")) + hintStyle.Render("  (Tab to complete)")
}

// startChoice opens a numbered settings picker. The selected item's apply runs
// when the user presses its digit (Esc cancels). Reusable for any quick toggle.
func (m *Model) startChoice(title string, items []choiceItem) {
	m.choiceItems = items
	m.choicePrompt = title
	m.setState(stateAwaitingChoice)
	m.appendLine(askStyle.Render(title))
	for i, it := range items {
		m.appendLine(toolStyle.Render(fmt.Sprintf("  %d) %s", i+1, it.label)))
	}
}

// currentMode returns the live permission mode, defaulting to ModeDefault.
func (m *Model) currentMode() permission.Mode {
	if m.sess != nil && m.sess.PermissionMode != "" {
		return permission.Mode(m.sess.PermissionMode)
	}
	return permission.ModeDefault
}

// modeChoices builds the permission-mode picker, marking the current mode.
func (m *Model) modeChoices() []choiceItem {
	cur := permission.Mode(m.sess.PermissionMode)
	items := make([]choiceItem, 0, len(permission.SelectableModes()))
	for _, mode := range permission.SelectableModes() {
		mode := mode
		label := mode.Label()
		if mode == cur {
			label += "  (current)"
		}
		items = append(items, choiceItem{
			label: label,
			apply: func() string {
				m.sess.PermissionMode = string(mode)
				return "Permission mode: " + mode.Label()
			},
		})
	}
	return items
}

// busyGuard reports whether a slash command must be refused because a turn is
// in flight. Display/config commands run fine while the model works, but ones
// that mutate the conversation history, change the run state, or start another
// turn would race the active turn — those call this first. what is the command
// label used in the hint (e.g. "/clear").
func (m *Model) busyGuard(what string) bool {
	if m.state == stateRunning {
		m.appendLine(toolStyle.Render("  " + what + " isn't available while Klaudia is working — press Esc to interrupt first."))
		return true
	}
	return false
}

// toggleGoalSetting flips goal-setting mode. Turning it on loads any existing
// spec (PRD.md / .klaudia/GOAL.md) or invites the user to describe the goal so
// the model can draft one on the next turn; turning it off readies the loop.
func (m *Model) toggleGoalSetting() (tea.Model, tea.Cmd) {
	if m.goalSetting {
		m.goalSetting = false
		m.appendLine(bannerStyle.Render("Goal-setting finished. /goal run [N] to start the loop."))
		return m, nil
	}
	m.goalSetting = true
	cwd := ""
	if m.sess != nil {
		cwd = m.sess.CWD
	}
	text, specPath, _ := goal.Read(cwd)
	if strings.TrimSpace(text) != "" {
		m.appendLine(bannerStyle.Render(fmt.Sprintf(
			"Goal-setting on. Loaded spec from %s:\n  %s\nRefine it in chat, /goal to finish, then /goal run to start.",
			specPath, oneLine(text, 100))))
	} else {
		m.appendLine(bannerStyle.Render(fmt.Sprintf(
			"Goal-setting on — no spec yet. Tell me what you're building and I'll draft %s. /goal to finish.",
			specPath)))
	}
	return m, nil
}

// startGoalLoop kicks off the "/goal run [N]" Ralph loop: requires a spec, moves
// onto a dedicated branch when possible, then runs up to N interruptible
// iterations (each re-invoking the agent with goal.IterationPrompt; subsequent
// iterations are started by the doneMsg hook). Reached only from an idle
// KeyMsg, so — like the normal turn-start — it must NOT arm waitForEvent (the
// single reader is already outstanding).
func (m *Model) startGoalLoop(args []string) (tea.Model, tea.Cmd) {
	cwd := ""
	if m.sess != nil {
		cwd = m.sess.CWD
	}
	specText, specPath, _ := goal.Read(cwd)
	if strings.TrimSpace(specText) == "" {
		m.appendLine(errStyle.Render("No goal spec found. Run /goal first to create one (PRD.md or .klaudia/GOAL.md)."))
		return m, nil
	}

	n := goal.DefaultIterations
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v <= 0 {
			m.appendLine(errStyle.Render("usage: /goal run [N]  (N = max iterations, a positive integer)"))
			return m, nil
		}
		n = v
	}
	if n > goal.MaxIterations {
		n = goal.MaxIterations
		m.appendLine(toolStyle.Render(fmt.Sprintf("  capped at %d iterations.", goal.MaxIterations)))
	}

	// Move onto a dedicated branch so iterations stay isolated and revertible.
	// Reuse the branch if it already exists (resume prior work) rather than
	// resetting it, so a second /goal run continues from earlier commits.
	m.loopBranch, m.loopBaseBranch = "", ""
	if cwd != "" {
		branch := goal.BranchName(specText)
		// Capture the branch we're starting from (the merge target) before we
		// switch, unless we're already sitting on the goal branch (a resume).
		if base, err := gitOutput(cwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
			if b := strings.TrimSpace(base); b != branch {
				m.loopBaseBranch = b
			}
		}
		args := []string{"checkout", "-b", branch}
		if _, err := gitOutput(cwd, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
			args = []string{"checkout", branch}
		}
		if out, err := gitOutput(cwd, args...); err != nil {
			m.appendLine(toolStyle.Render("  (not branching: " + strings.TrimSpace(out) + ")"))
		} else {
			m.loopBranch = branch
			m.appendLine(toolStyle.Render("  ↳ on branch " + branch))
		}
	}

	m.goalSetting = false
	m.loopTotal, m.loopRemaining, m.loopSpecPath = n, n, specPath
	m.loopStubFixing, m.loopVerifying = false, false
	m.appendLine(bannerStyle.Render(fmt.Sprintf("Goal loop: up to %d iterations against %s. Esc or /goal stop to halt.", n, specPath)))

	prompt := m.prepareFirstLoopTurn(specPath, n)
	m.setState(stateRunning)
	return m, m.startTurn(prompt)
}

// prepareFirstLoopTurn selects the first turn's prompt for /goal run: either
// a stub-fix turn (when the Progress tracker doesn't list every phase the body
// describes — fixing that up-front makes the mechanical CountUnchecked gate
// trustworthy for the rest of the run) or the normal iteration prompt. Side
// effects (loopStubFixing, scrollback) are confined here so the caller stays
// linear and tests can drive this without standing up a full Tea runtime.
func (m *Model) prepareFirstLoopTurn(specPath string, n int) string {
	if missing, err := goal.RequiresStubFix(specPath); err == nil && len(missing) > 0 {
		m.loopStubFixing = true
		m.appendLine(toolStyle.Render(fmt.Sprintf("  ⚠ Progress tracker missing stubs for: %s — repairing first.", strings.Join(missing, ", "))))
		m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ iteration 1/%d (stub fix)", n)))
		return goal.StubFixPrompt(specPath, missing)
	}
	m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ iteration 1/%d", n)))
	return goal.IterationPrompt(specPath)
}

// loopIsStall reports whether a finished iteration looks like an undetected
// failure — finished without error, but the model produced literally nothing
// (no text, no turn count, no output tokens). Anthropic's session-limit and
// some other throttling cases can manifest as a successful HTTP response with
// an empty stream rather than a 429, and without this check the loop happily
// decrements its budget for nothing while the user wonders why the spinner is
// spinning. All three signals must be zero — a turn that returned text but
// no tokens (e.g. a stubby test fixture) or no text but at least one inner
// turn (refusal, tool-only) is real work and shouldn't trip this.
func loopIsStall(res agent.Result, err error) bool {
	return err == nil && res.Text == "" && res.NumTurns == 0 && res.OutputTokens == 0
}

// loopNext updates goal-loop state after a turn finished and returns the prompt
// for the next loop turn, or "" to stop. The completion path runs a two-layer
// gate against premature <goal-complete/>:
//
//  1. Mechanical: count `- [ ]` lines in the spec. If any remain, reject the
//     claim and continue iterating — catches "model forgot one it could see".
//  2. Verification: if the mechanical gate passes, fire ONE final-review turn
//     that forces a holistic re-read (body vs Progress tracker vs git/tests).
//     Only completion from THAT turn ends the loop — belt-and-suspenders for
//     "tracker is missing rows that body phases never had" (the huedoku failure
//     mode that RequiresStubFix already tries to prevent up-front).
//
// Stub-fix and verification turns don't consume an iteration each: stub-fix
// happens before normal iteration starts (counted as iteration 1 in display
// only), and verification is a single check on top.
func (m *Model) loopNext(res agent.Result, err error) string {
	if m.loopWrapUp { // the wrap-up turn just finished
		m.loopWrapUp = false
		if err == nil {
			m.appendLine(toolStyle.Render(fmt.Sprintf("  summary written to %s — /goal run to resume.", filepath.Base(m.loopSpecPath))))
		}
		m.appendMergeHint()
		return ""
	}
	if err != nil {
		m.loopRemaining = 0
		m.appendLine(toolStyle.Render("  ⊘ goal loop halted."))
		m.appendMergeHint()
		return ""
	}
	// Stall: turn finished without error but produced nothing. Most often this
	// is the Anthropic API returning a successful-looking empty stream during
	// session-limit / quota throttling — without this check the loop would
	// blow through its budget on empty turns. Halt with a clear line so the
	// user can fix the underlying cause (wait for limit reset, switch model,
	// etc.) before re-running.
	if loopIsStall(res, err) {
		m.loopRemaining = 0
		m.appendLine(errStyle.Render("  ⊘ empty response from model — likely rate-limited or session-limit-hit. Stopping the loop."))
		m.appendMergeHint()
		return ""
	}
	// Stub-fix turn just finished — fall through to normal iteration for the
	// remaining budget (decrement happens in the default branch).
	if m.loopStubFixing {
		m.loopStubFixing = false
	}

	if goal.IsComplete(res.Text) {
		// Mechanical gate first.
		if n, cerr := goal.CountUnchecked(m.loopSpecPath); cerr == nil && n > 0 {
			m.appendLine(toolStyle.Render(fmt.Sprintf("  ✗ completion claim rejected: %d unchecked item(s) remain in %s", n, filepath.Base(m.loopSpecPath))))
			m.loopVerifying = false
			return m.continueIteration()
		}
		// If we just ran the verification turn and it also says complete, exit.
		if m.loopVerifying {
			m.loopVerifying = false
			done := m.loopTotal - m.loopRemaining + 1
			m.loopRemaining = 0
			m.appendLine(bannerStyle.Render(fmt.Sprintf("  ✓ goal complete (verified) in %d iteration(s).", done)))
			m.appendMergeHint()
			return ""
		}
		// Mechanical passed but verification hasn't fired yet — fire it once.
		// Doesn't decrement the budget; it's an insurance check, not new work.
		m.loopVerifying = true
		m.appendLine(toolStyle.Render("  completion claimed; running final verification…"))
		return goal.VerificationPrompt(m.loopSpecPath)
	}

	// Non-complete turn. If we just did a verification turn, the model decided
	// more work was needed — surface that and resume normal iteration.
	if m.loopVerifying {
		m.loopVerifying = false
		m.appendLine(toolStyle.Render("  verification flagged remaining work; continuing iteration."))
	}
	return m.continueIteration()
}

// continueIteration consumes one slot from loopRemaining and returns the prompt
// for the next iteration — or kicks off the wrap-up turn when the cap is hit.
// Centralised so the rejection path and the ordinary path can't drift apart.
func (m *Model) continueIteration() string {
	m.loopRemaining--
	if m.loopRemaining > 0 {
		next := m.loopTotal - m.loopRemaining + 1
		m.appendLine(toolStyle.Render(fmt.Sprintf("  ↻ iteration %d/%d", next, m.loopTotal)))
		return goal.IterationPrompt(m.loopSpecPath)
	}
	m.loopWrapUp = true
	m.appendLine(toolStyle.Render(fmt.Sprintf("  stopped after %d iteration(s); summarising progress to the spec…", m.loopTotal)))
	return goal.WrapUpPrompt(m.loopSpecPath)
}

// formatStats renders the /stats line. When the context limit is known, the
// caller passes the live estimated resident size (via compaction.EstimateTokens
// over current history) and we surface it as "context: ~R/L (P%, source)".
// Unknown limits omit the suffix rather than show a misleading percentage.
// Extracted so the test can pin the format without standing up a Model.
func formatStats(turns int, inTok, outTok int64, resident, ctxLimit int, ctxSource string) string {
	base := fmt.Sprintf("Session: turns=%d  input_tokens=%d  output_tokens=%d", turns, inTok, outTok)
	if ctxLimit <= 0 {
		return base
	}
	pct := float64(resident) / float64(ctxLimit) * 100
	return fmt.Sprintf("%s  context: ~%d/%d (%.0f%%, %s)", base, resident, ctxLimit, pct, ctxSource)
}

// appendMergeHint tells the user where the loop's work landed and how to merge
// it (only when the loop actually moved onto a branch).
func (m *Model) appendMergeHint() {
	if m.loopBranch != "" {
		m.appendLine(toolStyle.Render("  " + goal.MergeHint(m.loopBranch, m.loopBaseBranch)))
	}
}

// handleSlash dispatches a slash command. Commands run locally and never reach
// the model. Most are safe to run while a turn is in flight; the destructive
// ones guard with busyGuard.
func (m *Model) handleSlash(input string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(input)
	cmd := fields[0]
	args := fields[1:]

	switch cmd {
	case "/help", "/?":
		m.appendLine(bannerStyle.Render(slashHelp() + m.skillHelpLines()))
	case "/quit", "/exit":
		return m, tea.Quit
	case "/clear":
		if m.busyGuard("/clear") {
			break
		}
		m.transcript.Reset()
		m.rawBlocks = nil
		m.history = nil
		m.appendLine(bannerStyle.Render("Cleared conversation and screen."))
	case "/last":
		if strings.TrimSpace(m.lastResult) == "" {
			m.appendLine(bannerStyle.Render("No tool output yet."))
			break
		}
		if strings.Contains(m.lastResult, "```") {
			m.appendLine(m.markdown(m.lastResult))
		} else {
			m.appendLine(toolStyle.Render(m.lastResult))
		}
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
	case "/theme":
		if len(args) == 0 {
			if m.state == stateRunning {
				m.appendLine(toolStyle.Render("  use /theme <name> while Klaudia is working (the picker needs an idle session). Names: " + themeNames()))
				break
			}
			m.startChoice("Theme — choose Markdown render colours:", m.themeChoices())
			return m, nil
		}
		theme, ok := lookupTheme(strings.Join(args, " "))
		if !ok {
			m.appendLine(errStyle.Render("unknown theme " + strings.Join(args, " ") + ". Available: " + themeNames()))
			break
		}
		m.setTheme(theme.id)
		m.appendLine(bannerStyle.Render("Theme: " + theme.name))
	case "/goal":
		switch {
		case len(args) == 0:
			if m.busyGuard("/goal") {
				break
			}
			return m.toggleGoalSetting()
		case strings.EqualFold(args[0], "run"):
			if m.busyGuard("/goal run") {
				break
			}
			return m.startGoalLoop(args[1:])
		case strings.EqualFold(args[0], "stop"):
			if m.loopRemaining > 0 || m.loopWrapUp || m.turnCancel != nil {
				m.loopRemaining, m.loopWrapUp, m.loopStubFixing, m.loopVerifying = 0, false, false, false
				if m.turnCancel != nil {
					m.turnCancel()
					m.turnCancel = nil
				}
				m.appendLine(toolStyle.Render("  ⊘ goal loop stopped."))
			} else {
				m.appendLine(bannerStyle.Render("No goal loop running."))
			}
		case strings.EqualFold(args[0], "clear"):
			m.sess.Goal = ""
			m.appendLine(bannerStyle.Render("Standing goal cleared."))
		default:
			m.sess.Goal = strings.Join(args, " ")
			m.appendLine(bannerStyle.Render("Standing goal set; it will be re-stated each turn:\n" + m.sess.Goal))
		}
	case "/memory":
		// Memory is always an interface value; headless mode gets memory.Disabled()
		// which returns "" for reads and ErrDisabled for writes.
		m.appendLine(m.handleMemoryCommand(args))
	case "/mcp":
		var servers []MCPServerInfo
		if m.sess.MCP != nil {
			servers = m.sess.MCP.Servers()
		}
		if len(servers) == 0 {
			m.appendLine(bannerStyle.Render("No MCP servers configured. Add them in .mcp.json or .klaudia/.mcp.json."))
			break
		}
		var b strings.Builder
		b.WriteString("MCP servers:")
		items := make([]choiceItem, 0, len(servers))
		for _, s := range servers {
			s := s
			status := "● connected"
			if !s.Connected {
				status = "○ disconnected"
			}
			fmt.Fprintf(&b, "\n  %s  %s (%d tools)", status, s.Name, s.Tools)
			if s.Connected {
				items = append(items, choiceItem{label: "Disconnect " + s.Name, apply: func() string {
					if err := m.sess.MCP.Disconnect(s.Name); err != nil {
						return "disconnect failed: " + err.Error()
					}
					return "Disconnected " + s.Name
				}})
			} else {
				items = append(items, choiceItem{label: "Reconnect " + s.Name, apply: func() string {
					if err := m.sess.MCP.Reconnect(s.Name); err != nil {
						return "reconnect failed: " + err.Error()
					}
					return "Reconnected " + s.Name + " (its tools work again this session)"
				}})
			}
		}
		m.appendLine(bannerStyle.Render(b.String()))
		m.startChoice("Manage MCP servers (Esc to leave as-is):", items)
		return m, nil
	case "/stats":
		resident := compaction.EstimateTokens(m.history)
		m.appendLine(bannerStyle.Render(formatStats(m.statTurns, m.statIn, m.statOut, resident, m.sess.ContextWindow, m.sess.ContextWindowSource)))
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
			m.rememberPermission("allow", rule)
		} else {
			m.rememberPermission("deny", rule)
		}
	case "/status":
		model := m.sess.Model
		if model == "" {
			model = "(default)"
		}
		resume := ""
		if m.sess.SessionID != "" {
			resume = "\nresume: klaudia --resume " + m.sess.SessionID
		}
		m.appendLine(bannerStyle.Render(fmt.Sprintf("model=%s  permissions=%s  messages=%d%s",
			model, m.currentMode().Label(), len(m.history), resume)))
	case "/mode":
		if len(args) > 0 {
			want := permission.Mode(args[0])
			if !want.Valid() {
				m.appendLine(errStyle.Render("unknown mode " + args[0] + ". Try /mode with no argument to pick one."))
				break
			}
			m.sess.PermissionMode = string(want)
			m.appendLine(bannerStyle.Render("Permission mode: " + want.Label()))
			break
		}
		m.startChoice("Permission mode — choose how Klaudia asks before acting:", m.modeChoices())
		return m, nil
	case "/config":
		m.appendLine(bannerStyle.Render(m.renderConfig()))
	case "/agents":
		m.appendLine(bannerStyle.Render(m.renderAgents()))
	case "/context":
		m.appendLine(bannerStyle.Render(m.renderContext()))
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
		if m.busyGuard("/compact") {
			break
		}
		if m.sess.Compact == nil {
			m.appendLine(errStyle.Render("compaction is not available"))
			break
		}
		if len(m.history) == 0 {
			m.appendLine(bannerStyle.Render("Nothing to compact yet."))
			break
		}
		m.appendLine(bannerStyle.Render("Compacting conversation…"))
		m.setState(stateRunning)
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
		if m.busyGuard("/commit") {
			break
		}
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
		m.setState(stateAwaitingConfirm)
		// The y/n lives in the persistent bottom view (stateAwaitingConfirm);
		// scrollback just records the question and the diff being committed.
		m.appendLine(askStyle.Render("Stage all changes and commit?\n" + strings.TrimRight(status, "\n")))
	default:
		// A /<skill> matching a user-defined skill renders its body and submits it
		// as the turn prompt. Built-in commands above always win (a skill that
		// shadows one is unreachable here).
		if sk, ok := m.lookupSkill(strings.TrimPrefix(cmd, "/")); ok {
			if m.busyGuard("/" + sk.Name) {
				break
			}
			rendered := sk.Render(strings.Join(args, " "))
			m.appendLine(bannerStyle.Render("Running skill /" + sk.Name))
			m.setState(stateRunning)
			return m, m.startTurn(rendered)
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
	return fmt.Sprintf("Configuration:\n  provider=%s\n  model=%s\n  sandbox=%s\n  permissions=%s\n\n  /mode to change permissions · /model to change the model",
		provider, model, sandbox, m.currentMode().Label())
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
	sessionID := m.sess.SessionID
	if sessionID == "" {
		sessionID = "(unknown)"
	}
	return fmt.Sprintf("Context:\n  cwd=%s\n  git-branch=%s\n  session-id=%s\n  messages=%d", cwd, branch, sessionID, len(m.history))
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

// completeSlash fills the input with the unique command completion, or the
// common prefix when several match.
func (m *Model) completeSlash() {
	sug := m.slashSuggestions()
	if len(sug) == 0 {
		return
	}
	if len(sug) == 1 {
		m.input.SetValue(sug[0] + " ")
	} else {
		m.input.SetValue(commonPrefix(sug))
	}
	m.input.CursorEnd()
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

// fmtDuration renders a turn duration compactly: "850ms", "12.3s", "2m05s".
func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		return fmt.Sprintf("%dm%02ds", m, s)
	}
}

// throughput renders the token count and tokens/sec for a completed turn, or ""
// when no output tokens were reported (e.g. some OpenAI-compatible endpoints).
func throughput(outTokens int64, d time.Duration) string {
	if outTokens <= 0 {
		return ""
	}
	s := " · " + humanTokens(outTokens) + " tokens"
	if d > 0 {
		s += fmt.Sprintf(" · %.0f tok/s", float64(outTokens)/d.Seconds())
	}
	return s
}

// humanTokens renders a token count compactly: 980 → "980", 1240 → "1.2k".
func humanTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
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

// permissionSummary returns a one-line description of the pending action.
func (m *Model) permissionSummary(req agent.ApprovalRequest) string {
	label := req.ToolName
	if req.Specifier != "" {
		label += " (" + req.Specifier + ")"
	}
	switch req.ToolName {
	case "Edit":
		if target := firstNonEmpty(stringField(req.Input, "file_path"), req.Specifier); target != "" {
			return "edit " + target
		}
	case "Write":
		if target := firstNonEmpty(stringField(req.Input, "file_path"), req.Specifier); target != "" {
			return "write " + target
		}
	case "NotebookEdit":
		if target := firstNonEmpty(stringField(req.Input, "notebook_path"), req.Specifier); target != "" {
			return "edit notebook " + target
		}
	case "Bash":
		if desc := stringField(req.Input, "description"); desc != "" {
			return "run command — " + desc
		}
		if target := firstNonEmpty(req.Specifier, stringField(req.Input, "command")); target != "" {
			return "run command " + target
		}
	}
	return label
}

func (m *Model) permissionPrompt() string {
	return fmt.Sprintf("Allow %s? (y)es once / (a)lways / (n)o", m.permissionSummary(m.pendingReq))
}

func permissionDetail(req agent.ApprovalRequest) string {
	switch req.ToolName {
	case "Edit":
		return editPermissionDetail(req.Input)
	case "Write":
		if path := stringField(req.Input, "file_path"); path != "" {
			return "file: " + path
		}
	case "NotebookEdit":
		if path := stringField(req.Input, "notebook_path"); path != "" {
			return "notebook: " + path
		}
	case "Bash":
		if cmd := stringField(req.Input, "command"); cmd != "" {
			return "command: " + oneLine(cmd, 220)
		}
	}
	if req.Suggestion != "" {
		return req.Suggestion
	}
	return ""
}

func editPermissionDetail(raw json.RawMessage) string {
	path := stringField(raw, "file_path")
	oldText := stringField(raw, "old_string")
	newText := stringField(raw, "new_string")
	var parts []string
	if path != "" {
		parts = append(parts, "file: "+path)
	}
	if oldText != "" || newText != "" {
		parts = append(parts, fmt.Sprintf("replace %q → %q", oneLine(oldText, 80), oneLine(newText, 80)))
	}
	return strings.Join(parts, "\n  ")
}

func stringField(raw json.RawMessage, name string) string {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ""
	}
	v, ok := fields[name].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(v)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}

// rememberPermission records a permission rule for the current UI session and,
// when the project has a .klaudia directory, persists it to .klaudia/config.toml.
func (m *Model) rememberPermission(kind string, rule permission.Rule) {
	formatted := permission.FormatRule(rule)
	verb := "allow"
	if kind == "deny" {
		verb = "deny"
		m.sessionDeny = append(m.sessionDeny, rule)
	} else {
		m.sessionAllow = append(m.sessionAllow, rule)
	}

	persisted := false
	var err error
	if m.sess != nil && m.sess.CWD != "" {
		persisted, err = config.AppendProjectPermission(m.sess.CWD, kind, formatted)
	}
	msg := fmt.Sprintf("  → always %s %s (this session)", verb, formatted)
	if err != nil {
		msg += "; config save failed: " + err.Error()
	} else if persisted {
		msg += "; saved to .klaudia/config.toml"
	}
	m.appendLine(toolStyle.Render(msg))
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
	m.setState(stateRunning)
}

// startTurn runs the agent in a goroutine, delivering events via the channel,
// and returns the command that drives the spinner + elapsed stopwatch. The turn
// runs under a cancellable context so Esc can interrupt it. A standing /goal is
// re-stated to the model each turn (Ralph-style).
func (m *Model) startTurn(prompt string) tea.Cmd {
	// Belt-and-braces: zero the per-turn live tally so a path that skipped
	// doneMsg can't poison the next reconcile. The normal path also resets
	// these on doneMsg, so this is just a guard.
	m.turnLiveTurns, m.turnLiveIn, m.turnLiveOut = 0, 0, 0
	// Phase tracking: start every turn in "thinking" and mark the clock now
	// so the quiet-detector doesn't fire on a fresh turn that legitimately
	// takes 30s+ to receive its first event (some providers stream slowly).
	m.phase = "thinking"
	m.lastEventAt = time.Now()
	m.activeToolName = ""
	m.activeToolStart = time.Time{}
	// Frame the turn. Goal-setting interviews the user and drafts the spec; an
	// active loop's prompt is already goal.IterationPrompt (built by the caller),
	// so leave it untouched; otherwise re-state any standing /goal (drift guard).
	switch {
	case m.goalSetting && m.sess != nil:
		existing, specPath, _ := goal.Read(m.sess.CWD)
		prompt = goal.FacilitatorPrompt(specPath, existing) + "\n\nUser: " + prompt
	case m.loopRemaining > 0 || m.loopWrapUp:
		// prompt is the iteration / wrap-up prompt already; no framing added.
	case m.sess != nil && m.sess.Goal != "":
		prompt = fmt.Sprintf("Standing goal for this session: %s\n\nCurrent instruction: %s", m.sess.Goal, prompt)
	}
	approver := &uiApprover{events: m.events}
	asker := &uiAsker{events: m.events}
	planner := &uiPlanner{events: m.events}
	emit := func(ev agent.Event) { m.events <- eventMsg{ev} }
	ctx, cancel := context.WithCancel(m.ctx)
	m.turnCancel = cancel
	go func() {
		res, err := m.run(ctx, prompt, m.history, approver, asker, planner, emit)
		m.events <- doneMsg{res: res, err: err}
	}()
	return tea.Batch(m.spin.Tick, m.sw.Reset(), m.sw.Start())
}

func (m *Model) renderEvent(ev agent.Event) {
	// Bump the activity clock on every event — bottomView reads this to
	// decide whether to surface the "quiet for…" stuck-state suffix.
	m.lastEventAt = time.Now()
	switch ev.Type {
	case "assistant":
		m.appendText(ev.Text) // streamed deltas (raw until flushed)
		m.phase = "streaming"
	case "tool_use":
		m.flushAssistant() // the assistant message before a tool call is complete
		// Echo the salient input (#1) and, for mutating tools, a change preview (#2).
		m.appendLine(toolStyle.Render("⚙ " + ev.ToolName + toolSummary(ev.ToolName, ev.Input)))
		if diff := toolDiff(ev.ToolName, ev.Input); diff != "" {
			m.appendLine(diff)
		}
		m.phase = "running " + ev.ToolName
		m.activeToolName = ev.ToolName
		m.activeToolStart = time.Now()
	case "tool_result":
		m.flushAssistant()
		m.lastResult = ev.Content // keep the full result for /last (#3)
		s := strings.TrimSpace(ev.Content)
		if s == "" {
			s = "completed"
		}
		style := toolStyle
		prefix := "✓ " + ev.ToolName
		if ev.IsError {
			style = errStyle
			prefix = "✗ " + ev.ToolName
		}
		// Append the tool's wall-clock duration to its result line so users
		// can see whether a tool was fast or slow without reading timestamps.
		// Matched on ToolName so a misordered event can't suffix the wrong tool.
		if m.activeToolName != "" && m.activeToolName == ev.ToolName {
			prefix += " · " + fmtDuration(time.Since(m.activeToolStart))
		}
		prefix += ": "
		// Syntax-highlight fenced code in results (#5), else truncated plain text.
		// Skip the Markdown path for cat -n line-numbered output (Read previews):
		// glamour treats it as prose and reflows every line into one run-on block,
		// inlining the line numbers — show such results verbatim instead.
		if !ev.IsError && strings.Contains(s, "```") && len(s) <= 4000 && !looksLineNumbered(s) {
			m.appendLine(toolStyle.Render("  " + prefix))
			m.appendLine(m.markdown(s))
		} else {
			if len(s) > 240 {
				s = s[:240] + "…  " + hintStyle.Render("(/last for full output)")
			}
			m.appendLine(style.Render("  " + prefix + strings.ReplaceAll(s, "\n", "\n  ")))
		}
		m.activeToolName = ""
		m.phase = "thinking"
	case "compaction":
		m.flushAssistant()
		if ev.Content == "" {
			// Start of a slow (model-based) autocompact — show progress until
			// the matching done banner arrives. Microcompact never sends this.
			m.phase = "compacting"
		} else {
			// Completion banner. The compaction is already finished by the time
			// this arrives; what follows is the model turn, so leave the phase
			// as "thinking" rather than stranding it on "compacting" while we
			// wait on the (post-compaction, still large) request's first token.
			m.appendLine(bannerStyle.Render("· " + ev.Content))
			m.phase = "thinking"
		}
	case "usage":
		// One inner LLM call's usage. Update both the session counters and the
		// per-turn tally so doneMsg's reconciliation knows what we already
		// counted. No scrollback line — the status bar at the bottom shows
		// the running totals, and we don't want a stream of "+1234 tokens"
		// noise during an iteration.
		m.statTurns += ev.TurnDelta
		m.statIn += ev.InputDelta
		m.statOut += ev.OutputDelta
		m.turnLiveTurns += ev.TurnDelta
		m.turnLiveIn += ev.InputDelta
		m.turnLiveOut += ev.OutputDelta
		// Demote streaming → thinking when a usage event arrives mid-turn —
		// it signals the model has finished one inner call. Don't clobber a
		// "running <Tool>" phase, since a usage event between tool_use and
		// tool_result is just the assistant's pre-tool turn-finalisation.
		if m.phase == "streaming" {
			m.phase = "thinking"
		}
	}
}

// appendText buffers a streamed assistant delta (rendered raw live, then
// prettified through glamour on flush).
func (m *Model) appendText(s string) {
	m.streamBuf.WriteString(s)
	m.syncViewport()
}

// flushAssistant commits the buffered assistant message to the transcript,
// rendered as Markdown via glamour. No-op when nothing is buffered.
func (m *Model) flushAssistant() {
	if m.streamBuf.Len() == 0 {
		return
	}
	m.appendMarkdown(m.streamBuf.String())
	m.streamBuf.Reset()
}

// markdown renders s through glamour, falling back to the raw text on error or
// before the renderer is built.
func (m *Model) markdown(s string) string {
	if m.glam == nil {
		return s
	}
	out, err := m.glam.Render(s)
	if err != nil {
		return s
	}
	return strings.TrimRight(out, "\n")
}

// looksLineNumbered reports whether s is cat -n style output — the Read tool's
// format of a right-aligned line number, a tab, then content ("%6d\t%s").
// Markdown-rendering such preformatted text reflows every line into one run-on
// block and inlines the numbers, so the caller must show it verbatim. Requires
// every checked non-empty line (the first few) to match, so a genuine Markdown
// result that merely happens to contain a numbered line isn't misclassified.
func looksLineNumbered(s string) bool {
	checked, numbered := 0, 0
	for _, ln := range strings.SplitN(s, "\n", 6) {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		checked++
		i := 0
		for i < len(ln) && ln[i] == ' ' {
			i++
		}
		d := i
		for d < len(ln) && ln[d] >= '0' && ln[d] <= '9' {
			d++
		}
		if d > i && d < len(ln) && ln[d] == '\t' {
			numbered++
		}
	}
	return checked > 0 && numbered == checked
}

// renderQueuedHint composes the "queued: <message>" line shown under the
// input while the model is working. The message snippet is rendered in the
// user-input style (Bold/accent2) so it's scannable at a glance — the
// previous all-dim version was easy to miss during long turns, and the
// session that lost a "ive setup caddy…" message to a stuck Bash showed
// the cost. Wrapper text (label, key hints, line count) stays in hint
// style so the visual weight goes to the queued content itself.
func (m *Model) renderQueuedHint() string {
	snippet := oneline(m.queued, 60)
	label := hintStyle.Render("⏎ queued: ")
	body := userStyle.Render(snippet)
	tail := "  " + hintStyle.Render("(Enter sends · ↑ edits)")
	if lines := strings.Count(m.queued, "\n") + 1; lines > 1 {
		tail = "  " + hintStyle.Render(fmt.Sprintf("(%d lines · Enter sends · ↑ edits)", lines))
	}
	return label + body + tail
}

// phaseLabel returns the verb shown in the spinner row. Cancellation
// outranks everything (so Esc/queued-Enter gets the dominant feedback);
// otherwise we surface whatever phase renderEvent last set, defaulting to
// "working" before the first event has landed in a turn. When in an active
// tool, the elapsed wall-clock of that specific call is appended so a
// long-running tool is visible separately from the turn timer — for the
// "ran two quick tools, then this one stalled" case the spinner reads
// "running Bash 42s… 1m12s" and the user can tell the LATEST call is the
// stalled one rather than the whole turn.
func (m *Model) phaseLabel() string {
	switch {
	case m.cancelling:
		return "cancelling"
	case m.phase == "":
		return "working"
	case m.activeToolName != "":
		return "running " + m.activeToolName + " " + fmtDuration(time.Since(m.activeToolStart))
	default:
		return m.phase
	}
}

// buildGlamour (re)builds the Markdown renderer for the given viewport width.
//
// We use a fixed "dark" style rather than WithAutoStyle: auto-style queries the
// terminal for its background colour (an OSC escape + reply), which blocks
// inside Bubble Tea's input loop — Bubble Tea owns stdin, so the reply never
// reaches glamour and it hangs until its internal timeout (the "initializing…"
// freeze). "dark" is the safe default for developer terminals.
func (m *Model) buildGlamour(width int) {
	w := width - 2
	if w < 20 {
		w = 20
	}
	if r, err := glamour.NewTermRenderer(m.glamourThemeOption(), glamour.WithWordWrap(w)); err == nil {
		m.glam = r
		m.glamWidth = width
	}
}

// appendLine appends a full, already-styled line.
func (m *Model) appendMarkdown(s string) {
	m.rawBlocks = append(m.rawBlocks, transcriptBlock{text: s, markdown: true})
	m.transcript.WriteString(m.markdown(s) + "\n")
	m.syncViewport()
}

func (m *Model) appendLine(s string) {
	m.rawBlocks = append(m.rawBlocks, transcriptBlock{text: s})
	m.transcript.WriteString(s + "\n")
	m.syncViewport()
}

func (m *Model) rerenderTranscript() {
	if len(m.rawBlocks) == 0 {
		return
	}
	// Regenerate the intro banner so it picks up the new theme's chrome colours.
	if m.hasIntro && len(m.rawBlocks) > 0 {
		m.rawBlocks[0].text = intro(m.introModel, m.introBranch, m.introTagline)
	}
	m.transcript.Reset()
	for _, block := range m.rawBlocks {
		if block.markdown {
			m.transcript.WriteString(m.markdown(block.text) + "\n")
		} else {
			m.transcript.WriteString(block.text + "\n")
		}
	}
}

func (m *Model) syncViewport() {
	if !m.ready {
		return
	}
	// Committed transcript (glamour-rendered answers + styled lines) plus the
	// in-progress assistant message shown raw as it streams. Wrap to width;
	// lipgloss preserves ANSI and won't re-wrap lines already within width.
	content := m.transcript.String()
	if m.streamBuf.Len() > 0 {
		content += m.streamBuf.String()
	}
	wrapped := lipgloss.NewStyle().Width(m.vp.Width).Render(content)
	m.vp.SetContent(wrapped)
	// Follow new output to the bottom unless the user has scrolled up to read
	// history (see m.follow). Honoring the flag — rather than re-checking
	// AtBottom() here — keeps us pinned even when the viewport height changes
	// under us (e.g. the "working…" line appearing shrinks vp), which would
	// otherwise leave AtBottom() false and silently strand new content.
	if m.follow {
		m.vp.GotoBottom()
	}
}

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	if !m.ready {
		m.vp = viewport.New(w, h)
		m.ready = true
	}
	m.vp.Width = w
	m.input.SetWidth(w - 4)
	if m.glam == nil || m.glamWidth != w {
		m.buildGlamour(w)
	}
	// syncInputHeight does the state-aware height reservation (status bar, and
	// the working line + queued hint while running) and syncs the viewport.
	m.syncInputHeight()
}

func (m *Model) View() string {
	if !m.ready {
		return "initializing…"
	}
	return m.vp.View() + "\n" + m.bottomView()
}

// bottomView renders everything below the scrollback: the state-specific prompt
// or input area, then the persistent status bar. Its measured height is what
// relayout reserves, so the two can never drift.
func (m *Model) bottomView() string {
	var bottom string
	switch m.state {
	case stateRunning:
		label := " " + m.phaseLabel() + "… "
		hint := "  (esc to interrupt)"
		if m.cancelling {
			hint = "  (Ctrl+C to force quit)"
		}
		work := m.spin.View() + label + bannerStyle.Render(m.sw.View()) + hintStyle.Render(hint)
		// Surface "stuck" by tracking time since the last event the renderer
		// saw. Suppressed during cancellation — "cancelling… quiet for 12s"
		// is a worse signal than "cancelling…". 30s threshold picked so a
		// fast turn never sees the suffix; a long API call or tool-bound
		// stall does. Re-evaluated every spinner tick (~80ms).
		if !m.cancelling && !m.lastEventAt.IsZero() {
			if q := time.Since(m.lastEventAt); q > 30*time.Second {
				work += hintStyle.Render(fmt.Sprintf("  · quiet for %s", fmtDuration(q)))
			}
		}
		m.input.SetHeight(m.inputHeight())
		bottom = work + "\n" + m.input.View()
		if m.queued != "" {
			bottom += "\n" + m.renderQueuedHint()
		}
	case stateAwaitingPermission:
		bottom = askStyle.Render(m.permissionPrompt())
	case stateAwaitingAnswer:
		bottom = askStyle.Render(fmt.Sprintf("Choose 1-%d", len(m.askOptions)))
	case stateAwaitingPlan:
		bottom = askStyle.Render("Approve plan? (y)es / (n)o")
	case stateAwaitingConfirm:
		bottom = askStyle.Render("Confirm? (y)es / (n)o")
	case stateAwaitingChoice:
		bottom = askStyle.Render(fmt.Sprintf("Choose 1-%d", len(m.choiceItems))) + hintStyle.Render("  (esc to cancel)")
	default:
		m.input.SetHeight(m.inputHeight())
		bottom = m.input.View()
		if sug := m.slashSuggestionLine(); sug != "" {
			bottom += "\n" + sug
		}
	}
	// Persistent status bar at the very bottom, in every state.
	return bottom + "\n" + m.statusLine()
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
	p := tea.NewProgram(
		New(ctx, run, history, sess),
		tea.WithAltScreen(),
		// Capture the mouse so the wheel scrolls the viewport. Without this the
		// terminal's alternate-scroll mode turns the wheel into ↑/↓ arrow keys,
		// which would hijack input history / queued-message editing instead of
		// scrolling. (Shift+drag still selects text in most terminals.)
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
