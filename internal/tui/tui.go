// Package tui is the interactive terminal frontend, built on Bubble Tea. It is
// a peer of the headless and stream-json frontends: it drives the same agent
// core (RunFunc), consumes the Emitter event stream, and resolves permission
// asks via an in-UI prompt (implementing agent.Approver).
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/stopwatch"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/compaction"
	"github.com/greenthread-ai/klaudia/internal/mcp"
	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/plan"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// RunFunc drives one user turn against the agent core. It is the shared
// frontend contract — see agent.Turn for why the per-turn arguments are a struct
// rather than the nine positional parameters this alias replaced.
type RunFunc = agent.RunFunc

// Session is mutable state shared between the TUI and the RunFunc closure, so
// slash commands like /model can change settings for subsequent turns. The
// RunFunc should read these fields fresh on each call.
type Session struct {
	SessionID      string         // transcript/session id used by --resume
	Model          string         // model alias or full ID ("" = default)
	ResolvedModel  string         // concrete model id for display
	Effort         api.Effort     // how hard the model works per response ("" = provider default)
	PermissionMode string         // live mode (ExitPlanMode flips it out of "plan")
	Memory         memory.Store   // backs /memory; never nil — set to memory.Disabled() when unavailable
	Goal           string         // standing goal re-injected each turn (Ralph-style)
	Theme          string         // markdown render theme ("" = dark)
	EnterInserts   bool           // Return inserts a newline; alt+Return/ctrl+j submit
	Hyperlinks     bool           // OSC 8 links on URLs and file paths (see HyperlinksEnabled)
	FileLinks      string         // file-link URL format; "" = file:// (see fileLinkURL)
	Skills         []SkillCommand // user-defined skills dispatched as /<name>

	// Render-only context for /config and /context (set once at startup).
	Provider    string      // resolved provider ("anthropic" | "openai" | …)
	SandboxMode string      // resolved sandbox mode ("local" | "os" | "container")
	CWD         string      // working directory
	GitBranch   string      // current git branch (may be "")
	Agents      []AgentInfo // built-in sub-agent types, for /agents
	ExtraDirs   []string    // additional working dirs added via /add-dir
	// ContextWindow is the input-token limit /stats reports against; resolved
	// at startup via api.ResolveLimits (config override > provider > model
	// default > 0). Zero means "unknown"; /stats omits the usage ratio in that
	// case.
	ContextWindow       int
	ContextWindowSource string
	// MaxTokens is the output-token cap requested per turn, from the same
	// resolution. It is carried on the session rather than read from config by
	// the caller because /model changes it: Opus 5.5 allows 128k and an
	// unlisted OpenAI-compatible model gets the 8192 floor, and a switch
	// between them has to move the cap with it.
	MaxTokens       int
	MaxTokensSource string

	// Compact, if set, runs a model-based compaction of the given history and
	// returns the replacement history plus the summary. Backs /compact.
	Compact CompactFunc
	// Doctor, if set, returns a rendered environment diagnostic. Backs /doctor.
	Doctor func() string
	// ListModels, if set, enumerates the models the configured provider serves,
	// backing the /model picker. Nil when the provider can't enumerate them, in
	// which case /model stays type-the-name only.
	ListModels func(context.Context) ([]api.ModelInfo, error)
	// ResolveLimits sizes a model for /model: the startup resolver with config
	// overrides bound in, given whatever the picker already knows about the
	// model. Nil sizes from the offline table alone.
	ResolveLimits func(model string, known api.ModelInfo) api.ModelLimits
	// MCP, if set, lets /mcp inspect and reconnect/disconnect servers. May be nil.
	MCP MCPController
	// OnMCPReload, if set, registers a listener for the outcome of MCP config
	// hot reloads. The TUI calls it once at startup. Nil means reload failures
	// are not reported, which is what happened before it existed: an edit that
	// broke the config looked identical to one that worked.
	OnMCPReload func(func(MCPReloadEvent))
	// Executor runs `!` commands, shared with the Bash tool so a direct command
	// behaves the same as one Klaudia runs. Nil falls back to an unconfined
	// local process.
	Executor sandbox.Executor
	// Jobs, if set, backs /jobs, /logs, /restart and /stopjob: the session's
	// managed background processes. Nil when there is no job store.
	Jobs JobController
	// Trust, if set, backs /trust: the session's host guardrail, its approvals
	// and what the classifier has found. Nil when there is no gate, which
	// /trust reports rather than hiding.
	Trust TrustController
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

// MCPReloadEvent reports the outcome of one hot reload of the MCP config. It
// lives in internal/mcp now — every frontend gets these, not just this one.
type MCPReloadEvent = mcp.ReloadEvent

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
	stateAnsweringOther
	stateAwaitingPlan
	stateAwaitingConfirm
	stateAwaitingChoice
)

// otherAnswerLabel is the escape hatch appended to every AskUserQuestion.
//
// The model picks the options, which means a question can only ever offer the
// answers it already thought of — and the moment its framing is wrong, a
// numbered list is a trap: the user has to choose among four answers to the
// wrong question, or kill the turn. So the list always carries one more entry
// that the model did not write, and choosing it hands the keyboard back.
const otherAnswerLabel = "Something else — answer in your own words"

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

// modelsMsg carries the result of a background model-list fetch.
type modelsMsg struct {
	models []api.ModelInfo
	err    error
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
// baseStyle is the root of every chrome style. TabWidth(NoTabConversion) is the
// important part: lipgloss expands tabs to four spaces by default, which
// silently destroys the column structure of anything tabular a tool prints
// (kubectl, `go test`, TSV) and means a tab can never survive to the clipboard.
// Letting the literal tab through matches what the user would see running the
// command themselves.
func baseStyle() lipgloss.Style {
	return lipgloss.NewStyle().TabWidth(lipgloss.NoTabConversion)
}

var (
	userStyle = baseStyle()
	toolStyle = baseStyle()
	errStyle  = baseStyle().Foreground(lipgloss.Color("9"))
	// bangEchoStyle marks a command the user ran directly, so the transcript
	// distinguishes "I did this" from "Klaudia did this".
	bangEchoStyle = baseStyle()
	askStyle      = baseStyle()
	bannerStyle   = baseStyle().Faint(true)
	logoStyle     = baseStyle()
	hintStyle     = baseStyle().Faint(true).Italic(true)
	// warnStyle marks the one status segment that must not read as ordinary
	// chrome: a mode where nothing is being checked.
	warnStyle = baseStyle().Bold(true).Foreground(lipgloss.Color("9"))
	// noticeStyle marks a system notice about something that went wrong
	// invisibly — a dropped web search leaves no other trace in the
	// transcript, which is what made it a bug. Deliberately not bannerStyle:
	// faint is for things the user may ignore.
	noticeStyle  = baseStyle().Foreground(lipgloss.Color("3"))
	suggestStyle = baseStyle()
)

func init() { applyChromeTheme(defaultChromePalette) }

// applyChromeTheme recolours the accent chrome styles from a theme palette so
// the banner, pickers, prompts, and type-ahead follow /theme (not just the
// rendered Markdown). Called at startup and on every theme change.
func applyChromeTheme(p themePalette) {
	accent := lipgloss.Color(p.accent)
	accent2 := lipgloss.Color(p.accent2)
	muted := lipgloss.Color(p.muted)
	logoStyle = baseStyle().Bold(true).Foreground(accent)
	askStyle = baseStyle().Bold(true).Foreground(accent)
	suggestStyle = baseStyle().Foreground(accent2)
	userStyle = baseStyle().Bold(true).Foreground(accent2)
	bangEchoStyle = baseStyle().Bold(true).Foreground(accent)
	toolStyle = baseStyle().Foreground(muted)
	hintStyle = baseStyle().Faint(true).Italic(true).Foreground(muted)
	promptBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(muted).
		Padding(0, 1)
}

// Model is the Bubble Tea model for the interactive REPL.
type Model struct {
	run    RunFunc
	events chan tea.Msg
	ctx    context.Context

	input  textarea.Model
	spin   spinner.Model
	state  uiState
	ready  bool
	width  int
	height int
	// out queues finished blocks for printing into the terminal's own
	// scrollback (see scrollback.go). Drained once per Update cycle.
	out printQueue

	transcript transcriptLog // in-memory record of what we printed
	history    []anthropic.BetaMessageParam
	pending    chan permission.Decision
	pendingReq agent.ApprovalRequest
	// hostRedirect marks the pending answer as "no, do it differently" rather
	// than a plain refusal, so the echoed line invites the instruction the user
	// is about to type instead of announcing that Klaudia will carry on without
	// it. Cleared as it is read.
	hostRedirect bool
	// following is the job whose log is being tailed into scrollback, or "".
	// Follow prints into the terminal's own scrollback rather than a managed
	// region, which is why scrolling up during follow cannot be snapped back
	// down — there is nothing to snap.
	following string
	// pinned are paths the user asked to keep in context. Re-stated every turn
	// so they survive compaction, which is the only way "keep this in mind"
	// still means something forty turns later.
	pinned []string
	// checkpoints is the undo stack: the contents of files, as git blobs, from
	// just before Klaudia changed them.
	checkpoints checkpointStack
	// turnLabel names the operation being undone, so /undo says "Undo: fix the
	// refresh-token race" rather than "undo 2 files".
	turnLabel string
	// base is the working tree as it was when the session started, plus stamps
	// of what Klaudia has written since. It is what makes "your changes" and
	// "Klaudia's changes" distinguishable at all.
	base *baseline
	// lastFrame is the visual width of each line of the previously rendered live
	// region, and the terminal width it was rendered at. Both are needed to work
	// out how many physical rows that frame occupies after the terminal reflows
	// it — see reflowDeficit.
	lastFrame      []int
	lastFrameWidth int
	// pendingReflow is cursor motion prefixed to the next frame to undo a
	// resize's reflow desync.
	pendingReflow string
	// promptIsBang tracks whether the input marker is currently "$", so the
	// prompt function is only rebuilt when the answer changes.
	promptIsBang bool
	// executor runs `!` commands. Shared with the Bash tool so a direct command
	// behaves exactly like one Klaudia runs — same sandbox mode, same process
	// group, same environment.
	executor sandbox.Executor
	// pendingShellContext holds `!` commands and their output, folded into the
	// next prompt so "revert that" has a referent.
	pendingShellContext []string
	// turnTouched is this turn's slice of touched, for the completion block.
	// Reset at startTurn so the summary describes the turn rather than the
	// session.
	turnTouched map[string]bool
	// pendingPaths maps an in-flight tool_use id to the file it will write, so
	// ownership is claimed after the write rather than before it.
	pendingPaths map[string]string
	// pendingCommands maps an in-flight tool_use id to its Bash command line,
	// so a result can be attributed to the command that produced it.
	pendingCommands map[string]string
	// turnResultsFrom is the result-ring sequence at the start of this turn.
	turnResultsFrom int
	// turnReportsFrom is how many host-gate reports existed at the start of this
	// turn, so the completion block can describe this turn's blocks rather than
	// the session's.
	turnReportsFrom int
	// touched is the set of repo-relative paths Klaudia has modified this
	// session, so /commit can stage its own work and leave the user's alone.
	touched map[string]bool
	sess    *Session
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
	// plan is the last plan snapshot the loop sent; planCollapsed is the
	// Ctrl+T toggle. A copy, not the store: the store is written from the agent
	// goroutine and the snapshot is immutable, so reading it here takes no
	// lock. See planview.go.
	plan          plan.Snapshot
	planCollapsed bool
	// residentTokens is the last estimated input-context size, refreshed at
	// doneMsg after history is reconciled. The status bar reads this as a
	// `· ctx N%` indicator against sess.ContextWindow so users see context
	// pressure without having to type /stats. Computed once per turn end
	// rather than per render — compaction.EstimateTokens walks the whole
	// history and isn't cheap on long sessions.
	residentTokens int
	// results holds the full, untruncated content of recent tool results so
	// /last can reach past the newest one. The event stream is not truncated
	// upstream, so what's stored is exactly what the model saw.
	results resultRing
	// lastAssistantText is the raw Markdown of the most recent assistant
	// message, kept so /copy can work from source rather than scraping it back
	// out of rendered output. msgAccum collects it across progressive chunks.
	lastAssistantText string
	msgAccum          strings.Builder
	// pendingOSC holds a clipboard escape sequence to emit on the next frame.
	// Writing it through View keeps it ordered with respect to the renderer.
	pendingOSC string
	// steer holds what the user typed while Klaudia was working. The agent loop
	// drains it at its next safe point, so a correction lands before the next
	// consequential action rather than after the turn. Anything still pending
	// when the turn ends becomes the next turn instead.
	steer steerBox
	// interruptResend holds a queued message the user escalated to "interrupt
	// and send now". It is drained out of the steer box at the moment of the
	// interrupt — before the turn is cancelled — so a late Interject poll on the
	// agent goroutine cannot drain it into the turn being killed and strand it
	// unanswered. doneMsg sends it as the next turn.
	interruptResend agent.Interjection
	// Pending AskUserQuestion.
	askReply    chan string
	askOptions  []tools.AskOption
	askQuestion string
	askIdx      int  // highlighted row; len(askOptions) is the escape hatch
	askStray    bool // text was typed at the picker since the last move (see onKey)
	// Pending ExitPlanMode approval.
	planReply chan bool
	// Pending /commit-style confirmation: run on "y", returns a result line.
	confirmAction func() string
	// Pending local settings picker (e.g. /mode), see choice.go. choiceHits
	// indexes choiceItems: it is the subset matching choiceFilter, ranked, and
	// choiceIdx selects within it.
	choiceItems  []choiceItem
	choicePrompt string
	choiceFilter string
	choiceHits   []int
	choiceIdx    int
	// Input history: submitted prompts (newest last), navigated with Up/Down.
	// histPos == len(inputHistory) means "not navigating" (editing a fresh line).
	inputHistory []string
	histPos      int
	histDraft    string // the in-progress line stashed when navigating up
	// Verbatim payloads for pastes shown in the input as chips (see paste.go).
	// Session-scoped, because history recall re-shows a chip.
	pastes pasteStore
	// File-reference completion state (see fileref.go, pathmenu.go).
	paths       pathIndex
	recentPaths []string
	menu        pathMenu
	// nav indexes the conversation for /search, /errors and /outline.
	nav []navEntry
	// Elapsed-run stopwatch and per-turn cancel (Esc interrupts the model).
	sw         stopwatch.Model
	turnCancel context.CancelFunc
	// streamBuf holds the not-yet-printed part of the in-progress assistant
	// message; scan tracks how much of it is safe to commit to scrollback, and
	// chunked records whether this message has already flushed a chunk (so the
	// spacing between chunks matches a single render).
	streamBuf streamBuffer
	scan      streamScan
	chunked   bool
	glam      *glamour.TermRenderer
	glamWidth int
	// Intro banner inputs, so it can be regenerated (recoloured) on theme change.
	// introTagline is chosen once so it stays stable across regenerations.
	introModel, introBranch, introTagline string
	hasIntro                              bool
}

// New builds the model. ctx cancels in-flight turns when the program exits.
// history seeds the conversation when resuming a session (may be nil). sess is
// shared mutable settings (may be nil).
func New(ctx context.Context, run RunFunc, history []anthropic.BetaMessageParam, sess *Session) *Model {
	if sess == nil {
		sess = &Session{}
	}
	in := newPromptInput()
	in.KeyMap.InsertNewline = newKeymap(sess.EnterInserts).Newline

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
	}
	m.executor = sess.Executor
	// Capture the working tree before Klaudia touches anything. A file that is
	// dirty now and dirty later looks the same either way; only this tells them
	// apart.
	m.base = newBaseline()
	if sess.CWD != "" {
		if status, err := gitOutput(sess.CWD, "status", "--porcelain"); err == nil {
			m.base.capture(status)
		}
	}
	// A job dying at 14:02 and being noticed at 14:40 costs the half hour in
	// between, so the store reports exits straight into the event loop.
	if sess.Jobs != nil {
		events := m.events
		sess.Jobs.OnExit(func(st tools.JobStatus) {
			select {
			case events <- jobExitMsg{status: st}:
			default: // a full queue must never block the job's own goroutine
			}
		})
	}
	// A broken .mcp.json edit used to be silent: the watcher reloaded, failed,
	// and said nothing, so the config looked applied. Reload outcomes go down
	// the same channel as job exits, which is how the TUI is written to across
	// goroutines without touching the renderer.
	if sess.OnMCPReload != nil {
		events := m.events
		sess.OnMCPReload(func(ev MCPReloadEvent) {
			select {
			case events <- mcpReloadMsg{event: ev}:
			default: // never block the config watcher
			}
		})
	}
	// Colour the chrome for the session's theme before drawing the banner.
	applyChromeTheme(chromePaletteFor(m.currentThemeID()))
	model, branch := "", ""
	if sess != nil {
		model, branch = sess.displayModel(), sess.GitBranch
	}
	m.introModel, m.introBranch = model, branch
	m.introTagline, m.hasIntro = randomTagline(), true
	m.appendLine(m.introText())
	// A resumed session gets the operational picture instead of the
	// dirty-tree note: it subsumes it, and adds what did not survive.
	if st := m.buildResumeState(); len(history) > 0 && st.hasContent() {
		m.appendLine(bannerStyle.Render(st.render()))
	} else {
		m.warnIfDirtyAtStart()
	}
	return m
}

// setState changes the UI state and re-syncs the layout (the running and idle
// states reserve different numbers of bottom rows).
func (m *Model) setState(s uiState) {
	m.state = s
	m.syncInputHeight()
}

// editableInput reports whether the current state shows a real, growing text
// box, as opposed to a one-line prompt waiting on a keystroke.
//
// Single source of truth on purpose. This set was written out twice — once in
// inputHeight, once in the paste gate — and the copies drifted:
// stateAnsweringOther grew a box in one and not the other, so pasting into
// "answer in your own words" silently did nothing, in the very state whose
// answer is most likely to be a pasted log.
func (m *Model) editableInput() bool {
	switch m.state {
	case stateIdle, stateRunning, stateAnsweringOther:
		return true
	default:
		return false
	}
}

// slashCommandsLive reports whether a leading "/" in the box would actually run
// a command. True while idle and while the model works — the running path
// dispatches slash commands too — and false in "answer in your own words",
// where the text is an answer and a leading slash is just a character.
//
// Tab completion and the suggestion line both ask this, so neither can offer a
// command that the submit path in that state would not run.
func (m *Model) slashCommandsLive() bool {
	return m.state == stateIdle || m.state == stateRunning
}

// bangRunsHere reports whether Enter on a "!" line would actually run it as a
// shell command. Idle only: ! runs locally and its output becomes context for
// the next turn, and there is no next turn to put it in while one is already in
// flight. stateAnsweringOther sends its text as an answer, where a leading "!"
// is a character like any other.
//
// syncPromptMarker asks this as well as the submit path, so the "$" marker
// cannot promise an execution that Enter would not perform. It did: the marker
// is driven from syncInputHeight, which runs in every state, so typing "!ls"
// while the model worked showed "$" and then sent the text to the model.
func (m *Model) bangRunsHere() bool {
	return m.state == stateIdle
}

// displayModel returns the model name to show in the intro/status.
func (s *Session) displayModel() string {
	if s.Model != "" {
		return s.Model
	}
	return s.ResolvedModel
}

func (m *Model) Init() tea.Cmd {
	// Drain here too: New queued the intro banner before the program started.
	return tea.Batch(textarea.Blink, m.waitForEvent(), m.out.drainCmd())
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

// Update is a thin wrapper over update whose only job is to flush queued
// scrollback exactly once per cycle. Doing it at one choke point rather than
// threading a tea.Cmd back through the ~40 appendLine/appendMarkdown call sites
// (many of them in helpers that return a string or nothing) means a forgotten
// return can never silently swallow output.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	if out := m.out.drainCmd(); out != nil {
		return model, tea.Batch(cmd, out)
	}
	return model, cmd
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Work out the reflow deficit against the *previous* frame before
		// resize() overwrites the width it was drawn at.
		if d := reflowDeficit(m.lastFrame, msg.Width); d > 0 {
			m.pendingReflow = "\r" + ansi.CursorUp(d) + ansi.EraseScreenBelow
		}
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.onKey(msg)

	case eventMsg:
		m.renderEvent(msg.ev)
		return m, m.waitForEvent()

	case jobExitMsg:
		m.onJobExit(msg.status)
		return m, m.waitForEvent()

	case mcpReloadMsg:
		m.onMCPReload(msg.event)
		return m, m.waitForEvent()

	case followTickMsg:
		return m, m.onFollowTick(msg.ref)

	case bangResultMsg:
		return m, m.onBangResult(msg)

	case permissionMsg:
		m.setState(stateAwaitingPermission)
		m.pending = msg.reply
		m.pendingReq = msg.req
		if msg.req.HostChange != nil {
			for _, line := range hostCardLines(msg.req.HostChange) {
				m.appendLine(line)
			}
			return m, m.waitForEvent()
		}
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
		m.askIdx, m.askStray = 0, false
		// Only the question goes to scrollback; the options are a live picker
		// (askView), and answerAsk commits the one chosen. Scrollback keeps
		// what stays true: what was asked and what was answered.
		m.appendLine(askStyle.Render("? " + msg.question))
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
			// A turn can complete at the protocol level and still say nothing —
			// a refusal, or a truncation. Left alone that renders as "✓ done"
			// with no answer above it, which reads as a bug. Name what happened
			// and how to get out of it.
			if note := agent.TurnNote(msg.res.StopReason, !agent.TurnEndedEmpty(msg.res.Text)); note != "" {
				if agent.TurnEndedEmpty(msg.res.Text) {
					m.appendLine(errStyle.Render("  ⚠ " + note))
				} else {
					m.appendLine(hintStyle.Render("  ⚠ " + note))
				}
			}
			m.appendLine(bannerStyle.Render("  ✓ done in " + fmtDuration(elapsed) + throughput(msg.res.OutputTokens, elapsed)))
		}
		// Results before accounting: what changed and what was verified, if
		// anything was. A turn that only read files prints nothing here rather
		// than an empty ceremony.
		if s := m.turnSummaryBlock(); s != "" {
			m.appendLine(s)
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
		// A queued message becomes the next turn. Two sources, in priority
		// order: one the user escalated to "interrupt and send now" (captured at
		// interrupt time so a late agent poll can't strand it), then one queued
		// during a turn that ended naturally before the agent drained it.
		resend := m.interruptResend
		interrupted := !resend.Empty()
		m.interruptResend = agent.Interjection{}
		if resend.Empty() {
			resend = m.steer.drain()
		}
		if q := strings.TrimSpace(resend.Text); q != "" {
			m.pushHistory(q)
			m.appendLine(userStyle.Render("› ") + q)
			m.setState(stateRunning)
			// q is the chip form (kept short for the queued hint and ↑ recall);
			// expand it only on the way to the model.
			next := m.pastes.expand(q)
			// An interrupt kills the foreground command but leaves managed
			// background jobs running (they are session-scoped). The model was
			// mid-task when cut off, so tell it what is still up and let it
			// decide whether each still matters for the new instruction.
			if interrupted {
				if note := m.runningJobsNote(); note != "" {
					next = note + "\n\n" + next
				}
			}
			return m, tea.Batch(m.waitForEvent(), m.startTurn(next), stopSW)
		}
		m.setState(stateIdle)
		m.input.Focus()
		return m, tea.Batch(textarea.Blink, m.waitForEvent(), stopSW)

	case modelsMsg:
		if msg.err != nil {
			m.appendLine(errStyle.Render("model: " + api.FriendlyError(msg.err)))
			m.appendLine(hintStyle.Render("  You can still set one by name: /model <id>"))
			return m, nil
		}
		m.showModelPicker(msg.models)
		return m, nil

	case pagerDoneMsg:
		if msg.path != "" {
			os.Remove(msg.path)
		}
		if msg.err != nil {
			m.appendLine(errStyle.Render("pager: " + msg.err.Error()))
		}
		return m, nil

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

// Run starts the interactive program and blocks until the user quits. history
// seeds a resumed conversation (may be nil); sess holds mutable settings shared
// with the RunFunc closure (may be nil).
func Run(ctx context.Context, run RunFunc, history []anthropic.BetaMessageParam, sess *Session) error {
	// Deliberately NOT alt-screen and NOT mouse-capturing.
	//
	// Klaudia renders inline: finished output is printed into the terminal's own
	// scrollback and only the input and status bar are redrawn in place. That
	// keeps every terminal-native behaviour working — scroll, search, drag to
	// select, tmux copy mode, and a conversation that is still there after you
	// quit. Alt-screen would take all of those away (and would also make
	// tea.Println a no-op), and mouse capture sets DECSET 1002, which is
	// precisely what stops click-drag from selecting text.
	p := tea.NewProgram(New(ctx, run, history, sess))
	defer quietStandardLogger()()
	_, err := p.Run()
	return err
}
