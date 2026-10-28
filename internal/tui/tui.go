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

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/stopwatch"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/mcp"
	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
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
	MaxEffort      api.Effort     // config maxEffort: /effort may not go above it ("" = no cap)
	PermissionMode string         // live mode (ExitPlanMode flips it out of "plan")
	Memory         memory.Store   // backs /memory; never nil — set to memory.Disabled() when unavailable
	Goal           string         // standing goal re-injected each turn (Ralph-style)
	Theme          string         // markdown render theme ("" = dark)
	EnterInserts   bool           // Return inserts a newline; alt+Return/ctrl+j submit
	Hyperlinks     bool           // OSC 8 links on URLs and file paths (see HyperlinksEnabled)
	IdleCompact    bool           // compact near the threshold while idle (idlecompact.go)
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

	// Suggest, if set, asks the model one question outside the conversation
	// (no tools, nothing recorded). Backs /commit's drafted message.
	Suggest func(ctx context.Context, prompt string) (string, error)
	// Notify is which terminal notifications to send when Klaudia needs the
	// user (ParseNotify); the zero value sends none.
	Notify NotifyModes
	// OnNotify, if set, runs the Notification hook for the same moments.
	// Called on its own goroutine: a hook must not hold up the UI.
	OnNotify func(reason, message string)
	// OnRewind, if set, records that the conversation was cut back to its
	// first keep messages, so a resumed session stays rewound.
	OnRewind func(keep int)
	// Compact, if set, runs a model-based compaction of the given history and
	// returns the replacement history plus the summary. Backs /compact.
	Compact CompactFunc
	// SummariseBefore, if set, summarises head (the conversation before a
	// chosen prompt), records that the summary replaced everything but the
	// last kept messages, and returns head's replacement. Backs /rewind's
	// "summarise up to here".
	SummariseBefore func(ctx context.Context, head []anthropic.BetaMessageParam, kept int) (newHead []anthropic.BetaMessageParam, summary string, err error)
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
	// ApproveProject approves and starts the project servers being held for
	// approval, remembered until they change.
	ApproveProject() error
}

// MCPServerInfo is one MCP server's status for the /mcp view.
type MCPServerInfo struct {
	Name      string
	Connected bool
	Tools     int
	// Held is set for a project server waiting for approval; Detail is then
	// its command or URL, and Refused says the user declined it this session.
	Held    bool
	Refused bool
	Detail  string
	// ListErr is why a connected server's tool list could not be read.
	ListErr string
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

// summariseDoneMsg ends a "summarise up to here": head replaces the history
// before index at.
type summariseDoneMsg struct {
	head    []anthropic.BetaMessageParam
	summary string
	at      int
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

// chrome is a chrome style. Lip Gloss v2 honours NoTabConversion only for a
// single line: a multi-line string goes through its alignment step, which
// replaces every tab with four spaces regardless (getLines, lipgloss v2.0.6;
// charmbracelet/lipgloss#774 — drop this type when that is fixed).
// So chrome renders a multi-line string a line at a time, which these
// styles — colour and weight only, no width, padding or border — render
// identically.
type chrome struct{ lipgloss.Style }

func (c chrome) Render(strs ...string) string {
	s := strings.Join(strs, " ")
	if !strings.Contains(s, "\t") || !strings.Contains(s, "\n") {
		return c.Style.Render(s)
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = c.Style.Render(l)
	}
	return strings.Join(lines, "\n")
}

var (
	userStyle = chrome{baseStyle()}
	toolStyle = chrome{baseStyle()}
	errStyle  = chrome{baseStyle().Foreground(lipgloss.Color("9"))}
	// bangEchoStyle marks a command the user ran directly, so the transcript
	// distinguishes "I did this" from "Klaudia did this".
	bangEchoStyle = chrome{baseStyle()}
	askStyle      = chrome{baseStyle()}
	bannerStyle   = chrome{baseStyle().Faint(true)}
	logoStyle     = chrome{baseStyle()}
	hintStyle     = chrome{baseStyle().Faint(true).Italic(true)}
	// warnStyle marks the one status segment that must not read as ordinary
	// chrome: a mode where nothing is being checked.
	warnStyle = chrome{baseStyle().Bold(true).Foreground(lipgloss.Color("9"))}
	// noticeStyle marks a system notice about something that went wrong
	// invisibly — a dropped web search leaves no other trace in the
	// transcript, which is what made it a bug. Deliberately not bannerStyle:
	// faint is for things the user may ignore.
	noticeStyle  = chrome{baseStyle().Foreground(lipgloss.Color("3"))}
	suggestStyle = chrome{baseStyle()}
)

func init() { applyChromeTheme(defaultChromePalette) }

// applyChromeTheme recolours the accent chrome styles from a theme palette so
// the banner, pickers, prompts, and type-ahead follow /theme (not just the
// rendered Markdown). Called at startup and on every theme change.
func applyChromeTheme(p themePalette) {
	accent := lipgloss.Color(p.accent)
	accent2 := lipgloss.Color(p.accent2)
	muted := lipgloss.Color(p.muted)
	logoStyle = chrome{baseStyle().Bold(true).Foreground(accent)}
	askStyle = chrome{baseStyle().Bold(true).Foreground(accent)}
	suggestStyle = chrome{baseStyle().Foreground(accent2)}
	userStyle = chrome{baseStyle().Bold(true).Foreground(accent2)}
	bangEchoStyle = chrome{baseStyle().Bold(true).Foreground(accent)}
	toolStyle = chrome{baseStyle().Foreground(muted)}
	hintStyle = chrome{baseStyle().Faint(true).Italic(true).Foreground(muted)}
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
	// loopSpanOpen is set once a goal loop's first turn has reset the
	// completion-block markers, so later iterations accumulate into one block
	// for the whole run instead of each printing their own.
	loopSpanOpen bool
	// unsized holds blocks committed before the first resize (see commit).
	unsized []transcriptBlock
	// out queues finished blocks for printing into the terminal's own
	// scrollback (see scrollback.go). Drained once per Update cycle.
	out printQueue

	transcript transcriptLog // in-memory record of what we printed
	history    []anthropic.BetaMessageParam
	pending    chan permission.Decision
	pendingReq agent.ApprovalRequest
	// knownModels is the model list the provider last reported to /model,
	// kept so /model <id> can warn about an id the endpoint does not list
	// without a second lookup. Nil until the list has been fetched once.
	knownModels []api.ModelInfo
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
	// pendingRecent maps an in-flight tool_use id to the files it names, noted
	// as recent only if the call succeeds.
	pendingRecent map[string][]string
	// pendingTools are calls whose line waits for their result, in call order.
	pendingTools []*pendingTool
	// toolTime is this turn's wall-clock time with at least one tool call in
	// flight (overlapping calls counted once), and toolsSince when the current
	// stretch began. tok/s divides by the rest: a turn that waited 39s on a
	// job read "7 tok/s" for a model streaming at 150.
	toolTime   time.Duration
	toolsSince time.Time
	// readRun holds consecutive successful Reads until something else is
	// written, so they can share one line (flushReads).
	readRun []readEntry
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
	// quitConfirmed is set by a /quit that warned about running work; the
	// next /quit goes ahead.
	quitConfirmed bool
	// resumeNote tells the model, on the first turn after a resume, what did
	// not survive (resumeState.modelNote). The card says it to the user; the
	// model's history still says the jobs are running.
	resumeNote string
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
	// contextMeasured is set when this turn's usage reported the real prompt
	// size, which then stands in for the estimate at doneMsg.
	contextMeasured bool
	// idleSeq numbers idle spells, so an idle-compaction timer from one that
	// a turn has since ended does nothing (idlecompact.go).
	idleSeq int
	// turnSeq numbers turns; checkpoints carry the one that made them.
	// rewindPoints are the user's prompts this session, oldest first, for
	// /rewind (rewind.go).
	turnSeq      int
	rewindPoints []rewindPoint
	// rewindCompacted is set once a compaction dropped recorded prompts, so
	// an empty /rewind can say why.
	rewindCompacted bool
	// results holds the full, untruncated content of recent tool results so
	// /last can reach past the newest one. The event stream is not truncated
	// upstream, so what's stored is exactly what the model saw.
	results resultRing
	// lastAssistantText is the raw Markdown of the most recent assistant
	// message, kept so /copy can work from source rather than scraping it back
	// out of rendered output. msgAccum collects it across progressive chunks.
	lastAssistantText string
	msgAccum          strings.Builder
	// pendingRaw collects escapes for the terminal (OSC 52 clipboard,
	// notifications) when there is no renderer to queue them on: under tests.
	// See emitRaw.
	pendingRaw string
	// focusKnown is set once the terminal has reported focus at all, and
	// focused is what it last said. notifyAttention stays quiet only when
	// both say the user is looking.
	focusKnown, focused bool
	// screen draws the live region when running in a terminal; nil under
	// tests, which read frame directly.
	screen *inlineRenderer
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
	// otherFor says whose free-text box stateAnsweringOther is: a question's
	// "something else", or plan approval's "keep planning".
	otherFor otherTarget
	// Pending plan approval: the picker's highlighted row and stray-typing flag.
	planIdx   int
	planStray bool
	// loopPlan marks a run over the approved plan (/plan run, or approval's
	// "keep going") rather than over a goal spec; goalList is a goal run's
	// checklist, for the panel.
	loopPlan bool
	goalList plan.Snapshot
	// quitting is set by quit, so the last frame clears the live region.
	quitting bool
	// jobsTicking is set while a jobsTickMsg is scheduled (see jobsTick).
	jobsTicking bool
	// bg runs the Agent tool's run_in_background sub-agents (background.go).
	bg *agent.Background
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
	// choiceReturn is the state the picker hands back to when it closes:
	// stateRunning when it was opened over a turn that is still in flight.
	choiceReturn uiState
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
	// glamTables are renderers sized for individual tables (tableRenderer).
	glamTables map[int]*glamour.TermRenderer
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
		sw:      stopwatch.New(stopwatch.WithInterval(100 * time.Millisecond)),
		state:   stateIdle,
		history: history,
		sess:    sess,
	}
	// Background sub-agents live as long as the session, and report through
	// the event channel like everything else the TUI hears from other
	// goroutines. The send gives up once the session is over, so a task that
	// finishes during shutdown cannot block on a reader that has gone.
	m.bg = agent.NewBackground(ctx, func(t agent.BackgroundTask) {
		select {
		case m.events <- bgDoneMsg{task: t}:
		case <-ctx.Done():
		}
	})
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
		sess.Jobs.OnMatch(func(st tools.JobStatus, lines []string, dropped int) {
			select {
			case events <- jobMatchMsg{status: st, lines: lines, dropped: dropped}:
			default:
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
	if len(history) > 0 {
		st := m.buildResumeState()
		m.appendLine(bannerStyle.Render(st.render()))
		m.resumeNote = st.modelNote()
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
	if _, ok := msg.(jobsTickMsg); ok {
		m.jobsTicking = false // this tick fired; jobsTick below re-arms it if still needed
	}
	model, cmd := m.update(msg)
	cmds := []tea.Cmd{cmd, m.jobsTick()}
	if out := m.out.drainCmd(); out != nil {
		cmds = append(cmds, out)
	}
	return model, tea.Batch(cmds...)
}

func (m *Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyPressMsg:
		return m.onKey(msg)

	case tea.FocusMsg:
		m.focusKnown, m.focused = true, true
		return m, nil

	case tea.BlurMsg:
		m.focusKnown, m.focused = true, false
		return m, nil

	case tea.KeyboardEnhancementsMsg:
		keysDisambiguated = msg.SupportsKeyDisambiguation()
		m.input.KeyMap.InsertNewline = m.keymap().Newline
		return m, nil

	case tea.PasteMsg:
		// A bracketed paste arrives as one message carrying the whole payload.
		// It is handled before any state dispatch so that pasting into a y/n
		// prompt can't be read as an answer, and before the textarea so the
		// widget's sanitizer never sees it (see paste.go).
		m.quitArmed = false
		return m.onPaste(msg.Content)

	case eventMsg:
		m.renderEvent(msg.ev)
		return m, m.waitForEvent()

	case jobExitMsg:
		return m, tea.Batch(m.onJobExit(msg.status), m.waitForEvent())

	case jobMatchMsg:
		return m, tea.Batch(m.onJobMatch(msg), m.waitForEvent())

	case mcpReloadMsg:
		m.onMCPReload(msg.event)
		return m, m.waitForEvent()

	case followTickMsg:
		return m, m.onFollowTick(msg.ref)

	case bangResultMsg:
		return m, m.onBangResult(msg)

	case permissionMsg:
		m.closeChoiceForPrompt()
		// The card is about a call whose line is not written yet; write it
		// (and its diff) first, so the question has its context above it.
		m.flushAssistant()
		m.showPendingTools()
		m.setState(stateAwaitingPermission)
		m.pending = msg.reply
		m.pendingReq = msg.req
		if hostgate.CardOf(msg.req) != nil {
			m.notifyAttention("permission", "Klaudia needs your approval for a change to this machine")
			for _, line := range hostCardLines(hostgate.CardOf(msg.req)) {
				m.appendLine(line)
			}
			return m, m.waitForEvent()
		}
		m.notifyAttention("permission", "Klaudia needs your permission: "+oneline(m.permissionSummary(msg.req), 80))
		m.appendLine(askStyle.Render("Permission required: " + m.permissionSummary(msg.req)))
		detail := permissionDetail(msg.req)
		if m.toolShown(msg.req.ToolName) {
			detail = shownCallDetail(msg.req)
		}
		if detail != "" {
			m.appendLine(toolStyle.Render("  " + detail))
		}
		// The actionable prompt lives only in the persistent bottom view (see
		// bottomView/stateAwaitingPermission) — don't duplicate it in scrollback.
		return m, m.waitForEvent()

	case askMsg:
		m.closeChoiceForPrompt()
		m.setState(stateAwaitingAnswer)
		m.askReply = msg.reply
		m.askOptions = msg.options
		m.askQuestion = msg.question
		m.askIdx, m.askStray = 0, false
		// Only the question goes to scrollback; the options are a live picker
		// (askView), and answerAsk commits the one chosen. Scrollback keeps
		// what stays true: what was asked and what was answered.
		m.appendLine(askStyle.Render("? " + msg.question))
		m.notifyAttention("question", "Klaudia has a question: "+oneline(msg.question, 80))
		return m, m.waitForEvent()

	case bgDoneMsg:
		cmd := m.onBackgroundDone(msg.task)
		return m, tea.Batch(m.waitForEvent(), cmd)

	case planMsg:
		m.closeChoiceForPrompt()
		m.setState(stateAwaitingPlan)
		m.planReply = msg.reply
		m.planIdx, m.planStray = 0, false
		m.appendLine(askStyle.Render("Proposed plan:"))
		m.notifyAttention("plan", "Klaudia proposed a plan")
		m.appendMarkdown(msg.plan)
		// The approval picker lives only in the persistent bottom view (see
		// bottomView/stateAwaitingPlan); don't also write it to scrollback or
		// the user sees the same prompt twice.
		return m, m.waitForEvent()

	case doneMsg:
		elapsed := m.sw.Elapsed()
		stopSW := m.sw.Stop()
		m.turnCancel = nil
		m.steer.forgetShown()
		m.cancelling = false // the goroutine returned; we're past the cancel window
		m.flushAssistant()   // prettify the final answer through glamour
		m.abandonPendingTools()
		switch {
		case errors.Is(msg.err, context.Canceled):
			m.appendLine(toolStyle.Render(fmt.Sprintf("  ⊘ interrupted after %s", fmtDuration(elapsed))))
		case msg.err != nil:
			m.appendLine(errStyle.Render("error: " + config.Hints.FriendlyError(msg.err)))
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
			m.appendLine(bannerStyle.Render("  ✓ done in " + fmtDuration(elapsed) + throughput(msg.res.OutputTokens, elapsed-m.takeToolTime())))
		}
		// Goal loop: decide now whether another iteration follows, because
		// that decides whether this turn gets a completion block of its own.
		var loopPrompt string
		if m.loopRemaining > 0 || m.loopWrapUp {
			loopPrompt = m.loopNext(msg.res, msg.err)
		}
		// Results before accounting: what changed and what was verified, if
		// anything was. A turn that only read files prints nothing here rather
		// than an empty ceremony. Mid-loop it prints nothing either: the block
		// covers the whole run (see loopSpanOpen) and comes once, at the end —
		// five lines per iteration buried the iterations themselves.
		if loopPrompt == "" {
			if s := m.turnSummaryBlock(); s != "" {
				m.appendLine(s)
			}
		}
		if msg.res.Messages != nil {
			m.history = msg.res.Messages
		}
		// Refresh the cached resident-tokens estimate for the status bar's
		// `· ctx N%` indicator. Doing it here (after history is reconciled)
		// avoids walking history on every render, which gets expensive on
		// long sessions — the indicator updates once per turn, which is
		// the natural rhythm for context-pressure feedback anyway.
		if !m.contextMeasured {
			m.residentTokens = compaction.EstimateTokens(m.history)
		}
		// Reconcile live usage with the authoritative Result. If every "usage"
		// event made it through, these deltas are zero (no double-count); if
		// some were dropped due to channel pressure, this catches the gap up
		// to the final accurate totals.
		m.statTurns += msg.res.NumTurns - m.turnLiveTurns
		m.statIn += msg.res.InputTokens + msg.res.CacheReadInputTokens + msg.res.CacheCreationInputTokens - m.turnLiveIn
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
		if loopPrompt == "" && !m.loopWrapUp {
			m.loopPlan = false // whatever kind of run it was, it is over
		}
		if loopPrompt != "" {
			m.settleState(stateRunning)
			return m, tea.Batch(m.waitForEvent(), m.startTurn(loopPrompt), stopSW)
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
			m.settleState(stateRunning)
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
		m.settleState(stateIdle)
		m.input.Focus()
		m.notifyTurnEnd(elapsed, msg.err)
		return m, tea.Batch(textarea.Blink, m.waitForEvent(), stopSW, m.scheduleIdleCompact())

	case idleCompactMsg:
		return m, m.onIdleCompact(msg)

	case modelsMsg:
		if msg.err != nil {
			m.appendLine(errStyle.Render("model: " + config.Hints.FriendlyError(msg.err)))
			m.appendLine(hintStyle.Render("  You can still set one by name: /model <id>"))
			return m, nil
		}
		m.knownModels = msg.models
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

	case commitDraftMsg:
		m.setState(stateIdle)
		m.input.Focus()
		if msg.err != nil || msg.message == "" {
			why := "the model returned nothing"
			if msg.err != nil {
				why = config.Hints.FriendlyError(msg.err)
			}
			m.appendLine(errStyle.Render("Could not draft a message (" + why + "). /commit <message> to write your own."))
			return m, tea.Batch(textarea.Blink, m.waitForEvent())
		}
		m.appendLine(bannerStyle.Render("Message:\n  " + strings.ReplaceAll(msg.message, "\n", "\n  ")))
		m.confirmCommit(msg.plan, msg.message)
		m.appendLine(hintStyle.Render("  not right? n, then /commit <your message>"))
		return m, tea.Batch(textarea.Blink, m.waitForEvent())

	case summariseDoneMsg:
		m.onSummariseDone(msg)
		m.settleState(stateIdle)
		m.input.Focus()
		return m, tea.Batch(textarea.Blink, m.waitForEvent())

	case compactDoneMsg:
		if msg.err != nil {
			m.appendLine(errStyle.Render("compact: " + config.Hints.FriendlyError(msg.err)))
		} else {
			m.history = msg.history
			m.forgetRewindPoints()
			// The measured size was the pre-compaction prompt; until the next
			// call reports a new one, the estimate of what is left stands.
			m.residentTokens, m.contextMeasured = compaction.EstimateTokens(m.history), false
			m.appendLine(bannerStyle.Render("Compacted conversation. Summary:\n" + strings.TrimSpace(msg.summary)))
		}
		m.settleState(stateIdle)
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
	//
	// Frames are drawn by inlineRenderer rather than Bubble Tea's own renderer,
	// which cannot survive a resize (see inline.go).
	m := New(ctx, run, history, sess)
	r := newInlineRenderer(os.Stdout)
	m.screen, m.out.sink = r, r.print
	p := tea.NewProgram(rendered{m: m, r: r}, tea.WithOutput(mutedTTY{os.Stdout}))
	defer quietStandardLogger()()
	r.start()
	defer r.stop()
	_, err := p.Run()
	return err
}
