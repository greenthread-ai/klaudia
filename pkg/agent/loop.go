// Package agent implements the core agentic loop: call the model (streaming),
// process the response, dispatch any tool_use blocks to local tools, append the
// results, and repeat until the model stops or max turns is reached.
//
// This mirrors agentLoop in the JS reference (06-app-ui.js). Phase 1 covers the
// single-agent path with local tools; compaction, sub-agents, and server-side
// tools land in later phases.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
	"github.com/greenthread-ai/klaudia/pkg/schema"
	tools "github.com/greenthread-ai/klaudia/pkg/tool"
)

// Emitter receives streaming events for stream-json output. It is called for
// assistant text, tool_use, and tool_result events as they occur. May be nil.
type Emitter func(event Event)

// EventSchemaVersion versions Event as a wire format: the stream-json
// frontend writes Events verbatim, so their field names and Type values are a
// contract with every peer. Adding a Type or an optional field is compatible
// and keeps the version; renaming or removing either, or changing a field's
// meaning, bumps it. TestEventWireShape pins the current shape.
const EventSchemaVersion = 1

// The Event types. A frontend should ignore a Type it does not know, so
// adding one is not a breaking change.
const (
	EventAssistant      = "assistant"       // a delta of the model's text
	EventToolUse        = "tool_use"        // a tool call, before it runs
	EventToolProgress   = "tool_progress"   // a line of a long tool's (or sub-agent's) progress
	EventToolResult     = "tool_result"     // a tool call's result
	EventUsage          = "usage"           // token deltas for one model call
	EventCompaction     = "compaction"      // compaction started (no Content) or finished (Content)
	EventNotice         = "notice"          // something the user should know; never sent to the model
	EventPermissionMode = "permission_mode" // the permission mode changed
	EventPlan           = "plan"            // the plan changed; Plan is the full snapshot
	EventSteer          = "steer"           // a message the user sent mid-turn reached the model
)

// Event is a streaming event emitted during a run (stream-json mode).
type Event struct {
	Type      string `json:"type"`                  // one of the Event* constants
	Text      string `json:"text,omitempty"`        // assistant text
	ToolName  string `json:"tool_name,omitempty"`   // tool_use / tool_result
	ToolUseID string `json:"tool_use_id,omitempty"` // tool_use / tool_result
	Input     any    `json:"input,omitempty"`       // tool_use input
	Content   string `json:"content,omitempty"`     // tool_result content (what the model sees)
	IsError   bool   `json:"is_error,omitempty"`    // tool_result error flag
	// HostBlocked marks a tool_result that the host gate stopped, as distinct
	// from a tool that failed. The two look identical to the model — both are
	// error results it must respond to — but they are not the same event for the
	// user. A blocked `2>/dev/null` that the model simply routes around is a
	// non-event, and rendering it as a red failure was reported as Klaudia
	// looking broken while it was in fact working correctly.
	HostBlocked bool `json:"host_blocked,omitempty"`
	// FullContent is the untruncated tool output when the tool clamped Content
	// to protect the context window. Local frontends show this; it never goes
	// back to the model. Empty means Content is already complete.
	FullContent string `json:"full_content,omitempty"`
	// Data is the structured detail tools attached to their result
	// (tool.Result.Data), for frontends; it never goes to the model. Edit
	// reports "line", where its change starts, so a frontend can number the
	// hunk without re-reading a file the next call may be rewriting.
	Data map[string]any `json:"data,omitempty"`
	// Usage deltas for one inner-loop LLM call, emitted after each streamTurn so
	// the TUI/stream-json frontend can update token counters live during long
	// goal iterations rather than waiting for the whole Run to return. The
	// matching Result fields (NumTurns/InputTokens/OutputTokens) stay
	// authoritative — the TUI reconciles against them at doneMsg so dropped
	// usage events still settle correctly.
	InputDelta  int64 `json:"input_delta,omitempty"`
	OutputDelta int64 `json:"output_delta,omitempty"`
	TurnDelta   int   `json:"turn_delta,omitempty"`
	// ContextTokens is the size of the prompt that call sent: InputDelta plus
	// the cache reads and writes. With prompt caching, InputDelta alone is the
	// small uncached tail, so a frontend that showed it understated both the
	// context and the tokens used many times over.
	ContextTokens int64 `json:"context_tokens,omitempty"`
	// Plan is the session's plan after a change, on a "plan" event. A full
	// snapshot rather than a diff: frontends replace what they show, so a
	// dropped or reordered event cannot leave one drawing a list that never
	// existed.
	Plan *plan.Snapshot `json:"plan,omitempty"`
}

// Recorder persists conversation messages to a transcript as the loop runs.
// role is "user" or "assistant"; message is the raw Anthropic message JSON.
type Recorder interface {
	Record(role string, message json.RawMessage) error
}

// CompactionRecorder is a Recorder that can also mark where history was
// replaced by a summary. Optional, and asserted on the turn's own Recorder
// rather than reached through OnSummary: OnSummary is one per process, and an
// ACP process records each session to its own transcript.
type CompactionRecorder interface {
	RecordCompaction(summary string) error
}

// Options configures a single Run.
type Options struct {
	Prompt    string
	Model     anthropic.Model
	System    string
	MaxTurns  int   // 0 = unlimited
	MaxTokens int64 // 0 = model-aware default via api.MaxOutputTokens
	// MaxTokensSource is the api.LimitSource* label behind MaxTokens. It exists
	// for the truncation note: "max_tokens=8192" alone told nobody that 8192 was
	// the unknown-model floor rather than the model's cap, which is how Opus 5.5
	// ran under a stale table without anyone noticing. Empty is allowed and
	// prints the number alone.
	MaxTokensSource string
	Permission      permission.Context
	// Gate is checked before every tool call, ahead of the permission check
	// (see Gate). Nil is no gate — right for a loop running in a sandbox;
	// Klaudia's is internal/hostgate.
	Gate Gate
	// WorkingDir is the project root every tool operates relative to — the
	// directory Bash runs in and the default root for Grep/Glob. Empty means
	// the process cwd, which is only correct for callers that already chdir'd.
	WorkingDir string
	// BeforeEdit is called with the paths a mutating tool is about to change,
	// synchronously, immediately before the tool runs. The frontend uses it to
	// checkpoint the current contents for undo.
	//
	// It has to be synchronous and it has to be here rather than in the event
	// stream: a tool_use event reaches a frontend on a channel, so a snapshot
	// taken when the event arrives can race the write it was meant to precede
	// and capture the new contents.
	BeforeEdit func(tool string, paths []string)
	// Interject is polled between turns and after each tool batch. It returns
	// anything the user has typed since the last poll, and whether they asked
	// Klaudia to stop once the current step finishes. Nil means nothing can
	// interrupt, which is the right answer for headless.
	Interject func() Interjection
	// Approver resolves permission "ask" decisions. Supplied by the frontend
	// (headless/TUI/editor/SDK). If nil, DenyAll is used.
	Approver Approver
	// InitialMessages seeds the conversation when resuming a session. The new
	// Prompt (if any) is appended after them.
	InitialMessages []anthropic.BetaMessageParam
	// Recorder, if set, receives each user/assistant message for transcript
	// persistence. May be nil.
	Recorder Recorder
	// WebTools enables the server-side web_search and web_fetch tools (executed
	// by the Anthropic API, not locally).
	WebTools bool
	// Asker, if set, lets interactive tools (AskUserQuestion) prompt the user.
	Asker tools.Asker
	// Planner, if set, handles ExitPlanMode approval.
	Planner tools.Planner
	// ReadText, if set, is where the Read tool gets a text file's contents
	// instead of from disk. See Turn.ReadText for why a frontend would want
	// that; nil reads disk.
	ReadText func(ctx context.Context, path string, line, limit int) (string, error)
	// DeferredTools names tools withheld from the initial request (loaded on
	// demand once ToolSearch reveals them). Typically the MCP tools.
	DeferredTools map[string]bool
	// ContextWindow is the model's context size, used for autocompact
	// thresholds. 0 uses the package default.
	ContextWindow int
	// NoMicrocompact and NoAutocompact turn off the two compaction stages
	// (docs/compaction.md). Off includes the forced autocompact after the
	// provider rejects a request for size, which then fails the turn instead.
	NoMicrocompact bool
	NoAutocompact  bool
	// Effort is how hard the model works on each response: thinking depth,
	// tool calls and text together. api.EffortUnset (the zero value) sends
	// nothing and lets the provider apply its own default.
	Effort api.Effort
	// PartialMessages, if set, receives raw model stream events during the main
	// answer turn (not compaction summaries). The CLI wires this to a
	// stream_event emitter when --include-partial-messages is set. Nil by
	// default and for the TUI, so its single-reader invariant is untouched.
	PartialMessages func(anthropic.BetaRawMessageStreamEventUnion)
	// OnSummary, if set, is called with each compaction summary the loop
	// produces (autocompact). The CLI persists it alongside the transcript for
	// token-saving resume. May be nil.
	OnSummary func(summary string)
	// Hooks, if set, runs the user's configured shell commands at the four
	// lifecycle points. Nil — the common case — costs nothing: see hooks.go.
	Hooks Hooks
	// SubAgent marks a child run spawned by the Agent tool.
	//
	// Its only effect is on hooks, and only on UserPromptSubmit: a sub-agent's
	// prompt was written by the model, not the user, so a hook that pastes the
	// current ticket in front of what the user typed has nothing to attach
	// itself to. The tool hooks do fire for a child's calls, because the
	// alternative is a formatter that runs on every edit except the ones a
	// sub-agent made.
	SubAgent bool
	// Plan is the conversation's step list: the store TodoWrite and
	// ExitPlanMode write to (passed on as tools.Context.Plans), and the one the
	// loop announces with a "plan" event whenever it changes; see planevent.go.
	// Nil leaves those tools on the store they were built with.
	Plan *plan.Store
	// Hints are the host's words for its own settings, used where the loop
	// tells the user or the model how to change one (see api.Hints). The
	// zero value words that advice neutrally.
	Hints api.Hints
	// Background runs sub-agents started with run_in_background (passed on
	// as tools.Context.Background). Nil runs them in the foreground. A
	// spawned child never gets one: a background child of a background child
	// would report to nobody.
	Background tools.BackgroundRunner
	// Spill is where clamped tool output is kept in full: the backstop cap,
	// Bash's own clamp (via tools.Context.Spill) and microcompact's elisions
	// all write here, so every truncation notice can name a file the model can
	// read. The host picks the directory — the CLI passes <config
	// root>/outputs — because an embedded core must not choose where it
	// writes. The zero value clamps and elides without writing anything.
	Spill tools.SpillStore
	// FallbackModels are tried in order, for the rest of the Run, when the
	// model is unavailable (overloaded or a server error that survived the
	// client's retries). Empty means the error is reported, as before.
	FallbackModels []anthropic.Model

	// forkBase is the conversation up to and including the assistant turn
	// whose tools are being dispatched, handed to tools as
	// Context.Conversation (a forked sub-agent starts from it). Set by Run,
	// never by callers.
	forkBase []anthropic.BetaMessageParam
}

// Result is the outcome of a Run.
type Result struct {
	Text         string // concatenated final assistant text
	NumTurns     int
	StopReason   string
	InputTokens  int64
	OutputTokens int64
	// CacheReadInputTokens / CacheCreationInputTokens report prompt-cache usage
	// across the run: tokens served from cache (cheap) vs. tokens written to it.
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
	// Messages is the full conversation after the run (initial + this turn's
	// exchanges), so a caller can carry it forward as InitialMessages for the
	// next turn (used by the stream-json embedding frontend).
	Messages []anthropic.BetaMessageParam
}

// Loop drives the agentic loop against an API client and a tool registry.
type Loop struct {
	provider api.Provider
	tools    *tools.Registry

	// compactFailures counts automatic compactions that failed in a row.
	// After maxCompactFailures the threshold-driven one stops trying (each
	// attempt is a model call that fails the same way) until one succeeds or
	// /compact does; a forced compaction, after an overflow, always tries.
	compactFailures atomic.Int32

	// prefix is the system prompt, tools and betas of the last request sent,
	// which /compact (Compact) reuses so its summary shares the
	// conversation's cached prefix. Guarded by prefixMu.
	prefixMu sync.Mutex
	prefix   *requestPrefix
}

// requestPrefix is what precedes the messages in a request: tool definitions
// and the system prompt, with the betas sent alongside them. The prompt cache
// is keyed on tools, then system, then messages, so a summary request without
// them missed the cache on the entire history — the largest request a session
// sends, billed in full at the moment it is largest. The tools also define the
// tool_use blocks the history carries. (From PR #66.)
type requestPrefix struct {
	system []anthropic.BetaTextBlockParam
	tools  []anthropic.BetaToolUnionParam
	betas  []anthropic.AnthropicBeta
}

func (l *Loop) rememberPrefix(p requestPrefix) {
	l.prefixMu.Lock()
	defer l.prefixMu.Unlock()
	l.prefix = &p
}

func (l *Loop) lastPrefix() *requestPrefix {
	l.prefixMu.Lock()
	defer l.prefixMu.Unlock()
	return l.prefix
}

// New builds a Loop over a model provider (Anthropic, OpenAI-compatible, …).
func New(provider api.Provider, registry *tools.Registry) *Loop {
	return &Loop{provider: provider, tools: registry}
}

// Run executes the loop until the model stops calling tools or MaxTurns is hit.
func (l *Loop) run(ctx context.Context, opts Options, emit Emitter) (Result, error) {
	maxTokens, maxTokensSource := opts.MaxTokens, opts.MaxTokensSource
	fallbackNext := 0 // next index into opts.FallbackModels
	if maxTokens <= 0 {
		// Model-aware default: the flat 8192 was low enough that an ordinary
		// large write hit the output limit mid-tool-call and was dispatched with
		// empty arguments. ResolveLimits with no provider is the same table
		// lookup as api.MaxOutputTokens, but it also says whether the number
		// was a table hit or the floor — and the truncation note needs that.
		l := api.ResolveLimits(ctx, nil, string(opts.Model), 0, 0)
		maxTokens, maxTokensSource = int64(l.MaxOutputTokens), l.MaxOutputSource
	}

	// rec records a message and says so, once per run, when the transcript
	// cannot be written. The error used to be discarded: a full disk or a
	// removed sessions directory lost the session without a word, and the
	// user found out only when --resume had nothing to resume. (From PR #44.)
	recordWarned := false
	rec := func(role string, msg any) {
		if err := record(opts.Recorder, role, msg); err != nil && !recordWarned {
			recordWarned = true
			if emit != nil {
				emit(Event{Type: EventNotice, Content: fmt.Sprintf(
					"the session transcript could not be written (%v) — this conversation will not be resumable until that is fixed", err)})
			}
		}
	}

	// Deferred tools are withheld from the request until ToolSearch reveals them.
	betas := api.DefaultBetas
	if opts.WebTools {
		betas = append(append([]string{}, betas...), api.WebToolBetas...)
	}
	revealed := newRevealSet()

	// The system prompt is rebuilt per turn because the permission mode can
	// change mid-run — approving a plan flips it — and the model has to be
	// told; see mode.go.
	var modes modeTracker
	var plans planTracker

	// The loop-breaker counters: how many times each IDENTICAL tool call
	// (name+input) has failed within this Run, and how many consecutive
	// failures of the same SHAPE a tool has produced regardless of input.
	// Together they break a model out of retry loops where either the same
	// call or the same kind of mistake keeps recurring. Behind a mutex because
	// a concurrent group's calls reach them together; see failureState.
	fs := newFailureState()

	messages := append([]anthropic.BetaMessageParam{}, opts.InitialMessages...)

	// The two hooks that run before anything is sent. SessionStart fires once
	// for the process; UserPromptSubmit on every turn that carries a prompt.
	//
	// Their output is prepended to the user message rather than spliced into the
	// system prompt. Injected context is part of the conversation — it is true
	// at the moment it was gathered, and a branch name or ticket summary that
	// silently updated itself in the system prompt would retroactively rewrite
	// what the model was told three turns ago. Prepending also keeps the cached
	// prefix stable.
	var injected []string
	if opts.Hooks != nil {
		if opts.Hooks.ClaimSessionStart() {
			injected = appendHookContext(injected, fireHooks(ctx, opts, emit, HookInput{Event: HookSessionStart}))
		}
		if opts.Prompt != "" && !opts.SubAgent {
			hr := fireHooks(ctx, opts, emit, HookInput{Event: HookUserPromptSubmit, Prompt: opts.Prompt})
			if hr.Blocked {
				// The prompt is not sent. This is the one refusal the model is
				// never told about, because there is nothing to tell: no request
				// was made. The user is told, because they are the one whose
				// message was dropped and the hook they wrote is why.
				reason := strings.TrimSpace(hr.Reason)
				if reason == "" {
					reason = "a UserPromptSubmit hook refused it without giving a reason"
				}
				if emit != nil {
					emit(Event{Type: EventNotice, Content: "prompt not sent — " + reason})
				}
				return Result{StopReason: "blocked_by_hook", Text: reason, Messages: messages}, nil
			}
			injected = appendHookContext(injected, hr)
		}
	}
	if opts.Prompt != "" || len(injected) > 0 {
		userMsg := anthropic.NewBetaUserMessage(promptBlocks(opts.Prompt, injected)...)
		tellMode(messages, &userMsg, permission.CurrentMode(opts.Permission))
		messages = append(messages, userMsg)
		rec("user", userMsg)
	}

	var res Result
	halted := false
	// calib corrects the local token estimate using the input sizes the API
	// reports; overflowRecovered ensures the compact-and-retry path fires at
	// most once per Run, so a genuinely oversized request still surfaces.
	var calib compaction.Calibration
	overflowRecovered := false
	thinkingRecovered := false
	bindingRecovered := false // preserved-thinking 400 recovered once (stripThinking)
	maxTokensLowered := false
	// The last batch of a Run that stops on MaxTurns or a halt has no next turn
	// to report it, and the panel would show the step before until the user
	// typed again.
	defer announcePlan(opts.Plan, emit, &plans)
	for {
		res.NumTurns++

		// First thing in the turn, ahead of steering and compaction: the
		// previous batch's TodoWrite is what the user wants to see, and
		// autocompaction is a model call that can take a minute.
		announcePlan(opts.Plan, emit, &plans)

		// A correction the user typed mid-turn goes in before the request is
		// built, so the model sees it while deciding what to do next rather
		// than after it has done it. See steer.go.
		if in := pollInterjection(opts); !in.Empty() {
			if msg, ok := steerMessage(in); ok {
				messages = append(messages, msg)
				rec("user", msg)
				if emit != nil {
					emit(Event{Type: EventSteer, Content: in.Text})
				}
			}
			halted = halted || in.Halt
		}

		// Build the tool list for this turn: eager tools plus any deferred tools
		// revealed so far (via ToolSearch). Rebuilt per turn so reveals take
		// effect on the next request.
		toolParams, terr := l.buildToolParams(ctx, opts.DeferredTools, revealed.snapshot(), opts.Asker != nil)
		if terr != nil {
			res.Messages = messages
			return res, terr
		}
		if opts.WebTools {
			toolParams = append(toolParams, webToolParams()...)
		}

		mode := permission.CurrentMode(opts.Permission)
		announceMode(mode, emit, &modes)
		system := systemFor(opts.System)
		prefix := requestPrefix{system: system, tools: toolParams, betas: betas}

		// Compaction runs at the top of every turn (docs/compaction.md):
		// microcompact first (cheap, local), then autocompact (model-based) if
		// near the context limit. The summary request carries this turn's
		// system prompt and tools, so it reads the conversation from cache.
		messages = l.compact(ctx, messages, opts, maxTokens, emit, &calib, prefix, false)

		// Keep the repair, rather than repairing a copy on the way out. The
		// sanitised version used to be built for the request alone, so the
		// orphans stayed in the conversation and were rediscovered on every
		// later turn. Kept, the model's note about a dropped search
		// (droppedServerToolPlaceholder) is added once and stays. Sanitising is idempotent, so doing it here costs
		// nothing on a clean history.
		messages = sanitizeMessages(messages)

		// Repair any message with empty content (e.g. an old refusal recorded with
		// content: null) before sending — the Anthropic API otherwise rejects the
		// whole request with "messages.<i>.content: Field required".
		params := anthropic.BetaMessageNewParams{
			Model:     opts.Model,
			MaxTokens: maxTokens,
			Messages:  messages,
			System:    system,
			Tools:     toolParams,
			Betas:     betas,
		}
		// Only set when the user asked for a level. An empty OutputConfig is
		// omitzero, so an unset effort leaves the request byte-identical to
		// what it was before effort existed and the provider applies its own
		// default. Top-level effort is GA — no beta header belongs here.
		if opts.Effort != api.EffortUnset {
			params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(opts.Effort)
		}

		l.rememberPrefix(prefix)
		estimateAtSend := compaction.EstimateTokens(messages)
		assistant, finalText, err := l.streamTurn(ctx, params, emit, opts.PartialMessages)
		if err != nil && api.IsThinkingBindingError(err) && !bindingRecovered {
			// A thinking block failed preserved thinking's check on an
			// endpoint where drop_block could not be asked for: the history
			// before it changed since it was written. Resending is the same
			// 400 every time. Removing every thinking block is a leading-run
			// removal, which the check accepts; the model re-plans without
			// its earlier reasoning. (PR #63.)
			bindingRecovered = true
			messages = stripThinking(messages)
			if emit != nil {
				emit(Event{Type: EventNotice, Content: "earlier reasoning could not be replayed (the conversation before it changed) — continuing without it"})
			}
			continue
		}
		if err != nil && api.IsImmutableThinking(err) && !thinkingRecovered {
			// The last assistant message was repaired (an interrupted
			// server-tool call), and the API will not accept a message whose
			// thinking blocks changed. The original blocks are gone, so there
			// is nothing to restore: abandon that turn and take it again. Every
			// resend otherwise fails identically — the session is over.
			thinkingRecovered = true
			messages = DropLastAssistantTurn(messages)
			if emit != nil {
				emit(Event{Type: EventNotice, Content: "the previous reply could not be re-sent (its thinking blocks were repaired) — retaking that turn"})
			}
			continue
		}
		if limit, ok := api.MaxTokensTooLarge(err); ok && !maxTokensLowered && int64(limit) < maxTokens {
			// We asked for more output than this model allows. The cap we sent
			// comes from an offline table (or the user's config), and a wrong
			// entry fails every resend identically — so the session would be
			// over. The 400 names the real limit, so adopt it and retake the
			// turn. Once per Run: a second one means the number in the error is
			// not the number being rejected, and looping on that is worse than
			// reporting it.
			//
			// Ahead of the overflow branch because this is the more specific
			// classification of the same status code: compacting cannot help a
			// request refused for its max_tokens, and the measured wording is
			// one reword away from containing "maximum context".
			maxTokensLowered = true
			if emit != nil {
				emit(Event{Type: EventNotice, Content: fmt.Sprintf(
					"the model's output limit is %d tokens, not the %d requested — retrying this turn at %d%s",
					limit, maxTokens, limit, maxTokensAdvice(opts.Hints))})
			}
			maxTokens, maxTokensSource = int64(limit), api.LimitSourceRejected
			continue
		}
		if err != nil && api.IsContextOverflow(err) && !overflowRecovered {
			// Every resend of an over-limit conversation fails identically, so
			// without this the session is finished. Summarise and re-send once;
			// a second overflow is a real failure and is reported.
			overflowRecovered = true
			if emit != nil {
				emit(Event{Type: EventCompaction})
			}
			messages = l.compact(ctx, messages, opts, maxTokens, emit, &calib, prefix, true)
			if emit != nil {
				emit(Event{Type: EventCompaction, Content: "context overflowed the model's window — summarised and retried"})
			}
			continue
		}
		if err != nil && api.IsUnavailable(err) && !api.IsCutOff(err) && fallbackNext < len(opts.FallbackModels) {
			// The provider, not the request: overloaded, or a 5xx the client
			// already retried. Carry on with the next model in the chain for
			// the rest of the Run, refitted to it — an effort it does not take
			// is dropped, and a cap above its own is lowered — and say so,
			// because the answer now comes from a different model.
			from := opts.Model
			opts.Model = opts.FallbackModels[fallbackNext]
			fallbackNext++
			if api.EffortError(string(opts.Model), opts.Effort) != "" {
				opts.Effort = api.EffortUnset
			}
			if lim := api.ResolveLimits(ctx, nil, string(opts.Model), 0, 0); int64(lim.MaxOutputTokens) < maxTokens {
				maxTokens, maxTokensSource = int64(lim.MaxOutputTokens), lim.MaxOutputSource
			}
			if emit != nil {
				emit(Event{Type: EventNotice, Content: fmt.Sprintf("%s is unavailable (%s) — continuing on %s",
					from, api.Hints{}.FriendlyError(err), opts.Model)})
			}
			continue
		}
		if err != nil {
			// A stream that failed after text was shown — cut off by a stall or
			// dropped connection, or interrupted by the user — keeps that text,
			// so the history matches what the user read. See interrupted.go.
			// (PR #53.)
			if partial, ok := interruptedPartial(assistant, err); ok {
				messages = append(messages, partial)
				rec("assistant", partial)
			}
			res.Messages = messages
			return res, err
		}
		// The response reports the request's true input size: that is the only
		// honest number available, so feed it back into the estimate.
		calib.Observe(estimateAtSend, int(assistant.Usage.InputTokens+
			assistant.Usage.CacheReadInputTokens+assistant.Usage.CacheCreationInputTokens))
		res.StopReason = string(assistant.StopReason)
		res.InputTokens += assistant.Usage.InputTokens
		res.OutputTokens += assistant.Usage.OutputTokens
		res.CacheReadInputTokens += assistant.Usage.CacheReadInputTokens
		res.CacheCreationInputTokens += assistant.Usage.CacheCreationInputTokens
		res.Text = finalText
		// Live usage tick: emit per inner LLM call so frontends can update
		// counters during long iterations. TurnDelta=1 mirrors res.NumTurns
		// being incremented at the top of this loop iteration.
		if emit != nil {
			emit(Event{
				Type:        EventUsage,
				InputDelta:  assistant.Usage.InputTokens,
				OutputDelta: assistant.Usage.OutputTokens,
				TurnDelta:   1,
				ContextTokens: assistant.Usage.InputTokens + assistant.Usage.CacheReadInputTokens +
					assistant.Usage.CacheCreationInputTokens,
			})
		}

		// Add the assistant turn to the running conversation. Persisting to the
		// transcript is DEFERRED until we know it's either final (no tools) or
		// has its tool_result paired up — see the record calls below. Persisting
		// here, then dying mid-dispatch (Ctrl+C / SIGTERM / OOM), would leak an
		// orphan tool_use to disk and poison the next resume.
		messages = append(messages, historyParam(assistant))

		// pause_turn: the API paused a long-running server-side tool (e.g. web
		// search). Re-send the accumulated turn to let it continue. The
		// assistant has no local tool_use to pair, so record it now.
		//
		// It may still carry a server_tool_use whose result hasn't arrived — the
		// paused search itself. That is valid to send back mid-flight, but if the
		// turn is then interrupted, the transcript keeps the unpaired block and
		// every later request 400s on it. sanitizeMessages repairs that shape on
		// the way out (servertool.go, orphanServerToolUseIDs).
		if assistant.StopReason == "pause_turn" {
			rec("assistant", assistant)
			continue
		}

		// Collect tool_use blocks from this turn.
		toolUses := toolUseBlocks(assistant)
		if len(toolUses) == 0 {
			// Final (tool-less) answer: structurally fine on its own, record now.
			rec("assistant", assistant)
			res.Messages = messages
			if halted {
				res.StopReason = "user_halt"
			}
			return res, nil
		}
		if halted {
			// The wrap-up request came back wanting to do more work. The user
			// asked it to stop; honour that rather than let it carry on with a
			// polite acknowledgement.
			rec("assistant", assistant)
			res.StopReason = "user_halt"
			res.Messages = messages
			return res, nil
		}

		// Dispatch tools and append their results. reveal lets ToolSearch mark
		// deferred tools active for subsequent turns.
		reveal := revealed.add
		// A tool_use the model never finished emitting because the turn hit the
		// output-token limit arrives here with empty ("{}") arguments — the
		// stream layer patches its truncated, invalid JSON so Accumulate doesn't
		// crash (see api.repairInvalidToolInputs). Dispatching it would run the
		// tool with no arguments (e.g. Write with no file_path/content, which the
		// user saw as a cryptic schema error and a retry loop). Return an
		// actionable result instead so the model shortens or continues.
		truncID, wasTruncated := truncatedToolUseID(assistant)
		preempt := func(tu anthropic.BetaToolUseBlock) (string, bool) {
			if wasTruncated && tu.ID == truncID {
				return truncatedToolNote(maxTokens, maxTokensSource, opts.Hints), true
			}
			return "", false
		}
		// Capacity-capped, so a fork that appends to it cannot write into the
		// array this loop goes on using.
		opts.forkBase = messages[:len(messages):len(messages)]
		resultBlocks := l.dispatchAll(ctx, toolUses, opts, emit, reveal, fs, preempt)
		// The aggregate cap, after the per-result one. Results that each pass
		// the 30 KB budget still add up, and a turn is not limited to a few
		// calls; see batchcap.go.
		names := make([]string, len(toolUses))
		for i, tu := range toolUses {
			names[i] = tu.Name
		}
		capMessage(resultBlocks, names, opts.Spill)
		toolResultMsg := anthropic.NewBetaUserMessage(resultBlocks...)
		// A tool can change the mode (approving a plan does), so the next
		// request is told here, in the message carrying the results.
		tellMode(messages, &toolResultMsg, permission.CurrentMode(opts.Permission))
		messages = append(messages, toolResultMsg)

		// Now persist BOTH back-to-back. The window for process termination
		// between them is microseconds (two Recorder.Record calls), whereas the
		// dispatch above can be seconds-to-minutes — that gap is where orphans
		// used to land in the transcript.
		rec("assistant", assistant)
		rec("user", toolResultMsg)

		// The second poll point. Between the tool results and the next request
		// is the other place a user message can be appended without splitting a
		// tool_use/tool_result pair — and it is the one that matters, because a
		// long turn is mostly tool batches, not turn boundaries.
		if in := pollInterjection(opts); !in.Empty() {
			if msg, ok := steerMessage(in); ok {
				messages = append(messages, msg)
				rec("user", msg)
				if emit != nil {
					emit(Event{Type: EventSteer, Content: in.Text})
				}
			}
			halted = halted || in.Halt
		}
		if halted {
			// One more request so the model can report what it finished, then
			// stop. Ending here instead would leave the user to work out what
			// was completed from the tool results.
			res.StopReason = "user_halt"
		}

		if opts.MaxTurns > 0 && res.NumTurns >= opts.MaxTurns {
			res.StopReason = "max_turns"
			res.Messages = messages
			return res, nil
		}
	}
}

// elisionSpiller gives microcompact somewhere to put the tool results it
// removes, so an elision stays recoverable: the placeholder names the file and
// the model can read it. It uses the same store as the per-result cap, under
// the same 24h prune. A store with no directory yields a nil Spiller, which
// microcompact already reads as "nowhere to put it".
func elisionSpiller(s tools.SpillStore) compaction.Spiller {
	if s.Dir == "" {
		return nil
	}
	return func(content string) (string, bool) { return s.Spill("elided", content) }
}

// compact applies microcompact then (if near the limit) autocompact to the
// message list, each unless Options turns it off.
func (l *Loop) compact(ctx context.Context, messages []anthropic.BetaMessageParam, opts Options, maxTokens int64, emit Emitter, calib *compaction.Calibration, prefix requestPrefix, force bool) []anthropic.BetaMessageParam {
	// A provider that clears old tool results server-side for this
	// conversation is left to: eliding them here would edit history, which
	// invalidates preserved thinking (api/contextedit.go, PR #63).
	if clearer, ok := l.provider.(api.ToolResultClearer); ok && clearer.ClearsToolResults(messages) {
		opts.NoMicrocompact = true
	}
	if !opts.NoMicrocompact {
		if out, res := compaction.Microcompact(messages, elisionSpiller(opts.Spill)); res.Compacted {
			messages = out
			if emit != nil {
				emit(Event{Type: EventCompaction, Content: fmt.Sprintf("microcompact: elided %d old tool results (~%d tokens saved)", res.ElidedCount, res.TokensSaved)})
			}
		}
	}

	// The estimate is corrected by what the API actually charged for earlier
	// turns; uncorrected it runs low by more than the safety buffer, which is
	// how a session reached "prompt is too long" without ever tripping this.
	// force is set when the provider has already rejected the request for
	// size: the threshold is moot, we are over it by definition.
	if !opts.NoAutocompact &&
		(force || (l.compactFailures.Load() < maxCompactFailures &&
			compaction.ShouldAutocompact(calib.Scale(compaction.EstimateTokens(messages)), opts.ContextWindow))) {
		// Autocompact is a model call that runs silently (emit=nil), so signal
		// its start with a contentless "compaction" event — the frontend shows
		// "compacting…" for the duration. Microcompact above is instant and
		// skips this: it only drops the done banner below.
		if emit != nil {
			emit(Event{Type: EventCompaction})
		}
		out, err := l.autocompact(ctx, messages, opts, maxTokens, &prefix)
		switch {
		case err == nil:
			l.compactFailures.Store(0)
			messages = out
			if emit != nil {
				emit(Event{Type: EventCompaction, Content: "autocompact: summarized prior conversation"})
			}
		case ctx.Err() != nil:
			// The turn was cancelled; that is not compaction failing.
		default:
			// Said out loud because keeping the full history is the visible
			// half of this failure: the user sees no compaction and a context
			// that keeps growing, and otherwise has no way to know why. Every
			// failure, not only a cut-off summary: a silent one is what made
			// a session that had outgrown its window look merely stuck.
			// (From PR #28.)
			n := l.compactFailures.Add(1)
			if emit != nil {
				msg := "autocompact failed: " + err.Error() + " — kept the full history"
				if errors.Is(err, errSummaryTruncated) {
					msg = err.Error() + " — kept the full history instead of a partial summary"
				}
				if n >= maxCompactFailures {
					msg += fmt.Sprintf("; %d failures in a row, so automatic compaction is paused until /compact succeeds", n)
				}
				emit(Event{Type: EventNotice, Content: msg})
			}
		}
	}
	return messages
}

// maxCompactFailures is how many automatic compactions may fail in a row
// before the threshold-driven one stops being attempted.
const maxCompactFailures = 3

// maxSummaryShrinks bounds how many times a summary request that is itself
// too long is made smaller and retried.
const maxSummaryShrinks = 3

// Compact unconditionally summarizes the conversation via the model and returns
// the replacement history plus the summary text. Used by the TUI's /compact
// command (the loop's own autocompact runs automatically near the context
// limit). maxTokens is the session's resolved output cap; <= 0 sizes it from
// the model. Returns an error if the summary call fails, yields no text, or was
// cut off.
func (l *Loop) Compact(ctx context.Context, messages []anthropic.BetaMessageParam, model anthropic.Model, maxTokens int64, effort api.Effort) ([]anthropic.BetaMessageParam, string, error) {
	summary, err := l.summarize(ctx, messages, model, maxTokens, effort, l.lastPrefix())
	if err != nil {
		return nil, "", err
	}
	l.compactFailures.Store(0)
	return compaction.ReplaceWithSummary(summary), summary, nil
}

// Complete asks the model one question outside any conversation: no tools,
// no history, nothing recorded. It is for the frontend's small jobs — a commit
// message drafted from a diff — that should not become a turn in the session
// the user is having. Effort is capped as a summary's is (SummaryEffort):
// these are extraction, not reasoning. A reply cut off at maxTokens is an
// error, not a shorter answer.
func (l *Loop) Complete(ctx context.Context, prompt string, model anthropic.Model, maxTokens int64, effort api.Effort) (string, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(prompt))},
		Betas:     api.DefaultBetas,
	}
	if lvl := compaction.SummaryEffort(effort); lvl != api.EffortUnset {
		params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(lvl)
	}
	assistant, text, err := l.streamTurn(ctx, params, nil, nil)
	if err != nil {
		return "", err
	}
	if assistant.StopReason == "max_tokens" {
		return "", fmt.Errorf("the reply was cut off at %d tokens", maxTokens)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("the model returned nothing")
	}
	return text, nil
}

// autocompact summarizes the conversation via the model and replaces history
// with the summary. On error the caller keeps messages unchanged.
func (l *Loop) autocompact(ctx context.Context, messages []anthropic.BetaMessageParam, opts Options, maxTokens int64, prefix *requestPrefix) ([]anthropic.BetaMessageParam, error) {
	summary, err := l.summarize(ctx, messages, opts.Model, maxTokens, opts.Effort, prefix)
	if err != nil {
		return messages, err
	}
	if opts.OnSummary != nil {
		opts.OnSummary(summary)
	}
	// Without the boundary, --resume could not tell where the summary stops
	// and the rest of the transcript begins.
	if cr, ok := opts.Recorder.(CompactionRecorder); ok {
		_ = cr.RecordCompaction(summary)
	}
	return compaction.ReplaceWithSummary(summary), nil
}

// errSummaryTruncated: the summary hit its output cap and stopped mid-text.
var errSummaryTruncated = errors.New("compaction summary was cut off at the output limit")

// summarize is the one summary call behind both /compact and autocompact.
//
// It used to send a hard-coded max_tokens of 4096 and never look at the stop
// reason. A long session's summary — thinking included, which counts against
// the same cap — ran out mid-sentence ("**Goal m") and was accepted as the
// whole history. The session carried on from a summary missing everything
// after the cut, including the user's answers to questions it then asked
// again. So the cap is the session's own resolved one, and a summary that
// reached it is refused: an unsummarised history is recoverable, a silently
// truncated one is not.
// prefix, when known, is the conversation's own system prompt and tools, sent
// so the summary reads the history from cache; nil sends none, as before.
//
// The summary request is the whole history plus an instruction, so when
// compaction is forced because that history overflowed, the request overflows
// too. It is then made smaller (compaction.ShrinkForSummary: the older half
// dropped at a real user turn, or the longest text blocks halved) and retried
// up to maxSummaryShrinks times, and the summary says the oldest part is not
// in it. Before, it failed the same way each time and the session was
// finished. (From PR #28.) The history is sanitized like any request's.
func (l *Loop) summarize(ctx context.Context, messages []anthropic.BetaMessageParam, model anthropic.Model, maxTokens int64, effort api.Effort, prefix *requestPrefix) (string, error) {
	if maxTokens <= 0 {
		maxTokens = int64(api.MaxOutputTokens(string(model)))
	}
	dropped := 0
	for attempt := 0; ; attempt++ {
		req := compaction.BuildSummaryRequest(sanitizeMessages(messages), model, maxTokens, effort)
		req.Betas = api.DefaultBetas
		if prefix != nil {
			req.System, req.Tools = prefix.system, prefix.tools
			if len(prefix.betas) > 0 {
				req.Betas = prefix.betas
			}
		}
		assistant, _, err := l.streamTurn(ctx, req, nil, nil)
		if err != nil {
			if !api.IsContextOverflow(err) || attempt == maxSummaryShrinks {
				return "", err
			}
			smaller, n, ok := compaction.ShrinkForSummary(messages)
			if !ok {
				return "", err
			}
			messages, dropped = smaller, dropped+n
			continue
		}
		if assistant.StopReason == "max_tokens" {
			return "", fmt.Errorf("%w (%d tokens)", errSummaryTruncated, maxTokens)
		}
		summary := finalAssistantText(assistant)
		if summary == "" {
			return "", fmt.Errorf("compaction produced no summary")
		}
		if dropped > 0 {
			summary = fmt.Sprintf("(The oldest %d messages were too long to include in this summary and are not reflected in it.)\n\n%s", dropped, summary)
		}
		return summary, nil
	}
}

// streamTurn issues one streaming request via the provider, emitting
// assistant-text events as deltas arrive, and returns the assembled message.
// rawSink, if non-nil, receives the raw stream events for partial-message
// output; it is nil for compaction summary turns.
func (l *Loop) streamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, emit Emitter, rawSink func(anthropic.BetaRawMessageStreamEventUnion)) (anthropic.BetaMessage, string, error) {
	sink := api.StreamSink{
		OnText: func(delta string) {
			if emit != nil {
				emit(Event{Type: EventAssistant, Text: delta})
			}
		},
		OnRawEvent: rawSink,
		OnTransformations: func(ts []api.InputTransformation) {
			if note := droppedThinkingNote(ts); note != "" && emit != nil {
				emit(Event{Type: EventNotice, Content: note})
			}
		},
	}
	ctx, span := startChatSpan(ctx, params)
	assistant, err := l.provider.StreamTurn(ctx, params, sink)
	endChatSpan(span, assistant, err)
	if err != nil {
		return assistant, "", fmt.Errorf("stream: %w", err)
	}
	return assistant, finalAssistantText(assistant), nil
}

// dispatch runs one tool_use: lookup → permission → validate → execute, and
// returns the tool_result block to append to the conversation.
// unknownToolMsg builds the error for an unrecognised tool name. If the name is
// actually a sub-agent type (a common model mistake — calling "Plan" directly),
// it steers the model to the Agent tool instead of a dead-end "no such tool".
func (l *Loop) unknownToolMsg(name string) string {
	if agentTool, ok := l.tools.Lookup("Agent"); ok {
		if lister, ok := agentTool.(interface{ HasType(string) bool }); ok && lister.HasType(name) {
			return fmt.Sprintf("%q is a sub-agent type, not a tool. Launch it with the Agent tool: "+
				"Agent(subagent_type=%q, prompt=…).", name, name)
		}
	}
	// List the real tool names so a model that's improvised a name (e.g. "Find"
	// when it meant Glob/Grep) sees what's actually on offer and can self-correct
	// — without us having to maintain a fuzzy-match table per typo.
	all := l.tools.All()
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name())
	}
	sort.Strings(names)
	if len(names) == 0 {
		return fmt.Sprintf("No such tool available: %s", name)
	}
	return fmt.Sprintf("No such tool available: %s. Available tools: %s.", name, strings.Join(names, ", "))
}

// repeatedFailureMsg is returned when the model retries an identical tool call
// that has already failed repeatedly, instead of running it again. It tells the
// model plainly to stop repeating and what to try instead.
func repeatedFailureMsg(name string, n int) string {
	msg := fmt.Sprintf("You have already tried this exact %s call %d times and it failed the same way each time. "+
		"Stop repeating it — running it again will not change the result. Do something different.", name, n)
	switch name {
	case "Edit", "NotebookEdit":
		msg += " Use Read to get the file's current exact contents, then build old_string by copying the " +
			"text verbatim from what Read returns (including indentation and blank lines), or take a different approach."
	case "Write":
		msg += " Read the file first to see what is actually there before overwriting it."
	}
	return msg
}

// repeatFailureLimit is how many times an identical tool call (same name and
// input) may fail within one Run before dispatch stops re-running it and
// instead steers the model to change tack. Weaker models otherwise retry the
// exact same failing call indefinitely, burning the whole turn budget.
const repeatFailureLimit = 2

// errStreak counts how many consecutive failures of the same SHAPE a tool has
// produced (same exact error message). It's reset whenever the tool succeeds
// or produces a different error. The point: identical-args retry detection
// misses "same mistake, different values" — like Read called repeatedly with
// `line_start`/`line_end` (wrong field names) but new line numbers each time.
//
// `firstInput`/`varied` track whether the model actually changed its inputs
// across the failing calls. If it did, and the tool actually RAN, the failure
// is almost certainly environmental (shell wedged, network down, disk full) —
// the model is varying its guesses in good faith and still hitting the same
// wall. `preExec` is the veto on that inference: a call rejected before the
// tool ran cannot have been defeated by the environment. shortCircuit uses the
// two flags to choose between the directive messages.
type errStreak struct {
	sig        string // last error message text
	count      int    // consecutive occurrences of sig
	firstInput string // raw JSON of the input on the first failure in this streak
	varied     bool   // true once a same-sig failure arrived with a different input
	preExec    bool   // the failures never reached the tool: bad name, bad input, denied
}

// failureKind says where a failure came from, which is what decides the
// conclusion the loop-breakers are allowed to draw from a run of them.
type failureKind int

const (
	// failureExec: the tool ran and failed. A wedged environment is a candidate.
	failureExec failureKind = iota
	// failurePreExec: refused before the tool ran — unrecognised name, input
	// that doesn't validate, permission denied. Nothing about the environment
	// is implicated, so these must never produce the "shell wedged" directive.
	failurePreExec
	// failureHostGate: refused by the host gate. A decision about one call,
	// not a malfunction; kept out of the shape streak entirely.
	failureHostGate
	// failurePermission: refused by the permission check — plan mode's
	// read-only rule, or the frontend's Approver answering no.
	//
	// Separate from failurePreExec because the conclusion the breakers may draw
	// is different again. A bad input says the call was wrong; a permission
	// refusal says only that policy forbade it *at that moment*, and the policy
	// is the one thing in the loop that a user changes mid-Run. Counting it is
	// what made an approved plan's first command be refused with the plan-mode
	// error it had already escaped. Neither breaker counts it; MaxTurns remains
	// the backstop against a model that will not stop asking.
	failurePermission
)

// bumpErrStreak records a new failure for tool. If the message matches the
// prior signature, the count grows and the input-varied flag tracks whether
// the model is changing inputs across calls. Otherwise the streak starts over.
func bumpErrStreak(streaks map[string]errStreak, tool, msg, input string, preExec bool) {
	prev := streaks[tool]
	if prev.sig == msg {
		prev.count++
		if input != prev.firstInput {
			prev.varied = true
		}
		prev.preExec = prev.preExec || preExec
	} else {
		prev = errStreak{sig: msg, count: 1, firstInput: input, preExec: preExec}
	}
	streaks[tool] = prev
}

// blocks reports whether an incoming call with this input should be refused by
// loop-breaker B, given a streak that has reached the limit.
//
// The distinction is what the streak is evidence FOR. A streak whose failures
// all carried the same input says "this call is wrong" — and a call with a
// different input is not that call, so it gets to run. A streak whose failures
// carried different inputs already says the error survives changing the input,
// which is the whole claim of envFailureMsg and of the "same mistake, new
// values" case (Read with `line_start` and fresh line numbers each time); there
// one more variation is exactly what must not happen.
//
// Without this the breaker fired at the moment the model fixed the problem.
// Real case: two Reads of a macOS screenshot path failed identically (the real
// filename holds U+202F, not a space), the model diagnosed it, copied the file
// to /tmp/wf.png and read THAT — and was answered with the stale error about
// the Desktop path, which reads as "the file you just created does not exist".
func (e errStreak) blocks(input string) bool {
	return e.varied || input == e.firstInput
}

// bareExitStatus matches a result that is nothing but Bash's exit-status line.
var bareExitStatus = regexp.MustCompile(`^\[exit code -?\d+\]$`)

// streakSignature is the "shape" a failure is compared on.
//
// Usually the error text: identical text across different inputs is what
// suggests the environment rather than the call. A silent non-zero exit has
// no text — just "[exit code 1]" — so it is identical for every silent
// failure there is: grep with no match, a failing test, diff -q finding a
// difference. Two of those in a row read as a wedged shell, and the breaker
// then refused the model's next command — in the case that found this, the
// `git status` it was running to see which edits had landed. A bare exit
// status says nothing about the environment, so it is told apart by its
// input: the same command failing twice still counts, different ones do not.
func streakSignature(content, input string) string {
	if bareExitStatus.MatchString(strings.TrimSpace(content)) {
		return strings.TrimSpace(content) + "\x00" + input
	}
	return content
}

// repeatedShapeFailureMsg is the directive returned when loop-breaker B fires
// AND the model kept submitting the same input — classic "stop guessing" case.
// Quoting the recurring error forces the model to confront the real problem
// rather than retry with cosmetic variations.
func repeatedShapeFailureMsg(tool string, n int, sig string) string {
	return fmt.Sprintf(
		"%s has failed with the SAME error %d times in a row. Stop calling it until you've understood the error below — guessing different values for the same wrong call won't help.\n\n--- recurring error ---\n%s",
		tool, n, sig,
	)
}

// envFailureMsg is the directive returned when loop-breaker B fires, the model
// HAD varied its inputs across the failing calls, and the tool actually ran
// each time. The error shape is stable while the inputs aren't — the env, not
// the call, is broken. Telling the model to "stop guessing" in this case is
// actively misleading (it WAS trying different things) and pushes it into
// useless workaround loops. Suggest concrete recovery moves instead.
//
// This must not fire for failures that never reached the tool. A rejected input
// is stable across varied inputs for the obvious reason, and answering it with
// "the shell may be wedged, try KillShell" sends the model to reset a substrate
// that was never involved instead of reading the error.
func envFailureMsg(tool string, n int, sig string) string {
	return fmt.Sprintf(
		"%s has failed %d times in a row with the same error shape across DIFFERENT inputs. This looks like an environment issue (shell wedged, leaked background process, network unreachable, filesystem broken) rather than a tool-input problem — varying the inputs more won't help. Options: (a) reset the relevant state (e.g. KillShell for a stuck Bash; restart a stuck server); (b) try a different tool that doesn't depend on the broken substrate; (c) ask the user. Stop retrying %s with new inputs.\n\n--- recurring error ---\n%s",
		tool, n, tool, sig,
	)
}

// editedPaths returns the files a mutating tool is about to change, or nil if
// it is not one. Kept next to dispatch because the list of mutating tools is a
// property of the loop, not of any one tool.
func editedPaths(name string, raw []byte) []string {
	switch name {
	case "Write", "Edit", "NotebookEdit":
	default:
		return nil
	}
	var in struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil
	}
	for _, p := range []string{in.FilePath, in.NotebookPath} {
		if p != "" {
			return []string{p}
		}
	}
	return nil
}

// shortCircuit returns a tool_result without touching the failure counters
// (the loop-breaker is refusing the call, not registering another attempt).
// Used by loop-breaker B; A reuses errResult since it bumps state intentionally.
// truncatedToolUseID reports the id of a tool_use the model never finished
// emitting because the turn hit the output-token limit. max_tokens truncates
// only the final content block, so a trailing tool_use whose arguments never
// closed is the casualty; the stream layer patches its invalid JSON to "{}"
// (api.repairInvalidToolInputs) to keep Accumulate from crashing, which is the
// empty input keyed on here. Only the final block qualifies, so a complete
// tool_use earlier in the same turn still dispatches normally.
func truncatedToolUseID(m anthropic.BetaMessage) (string, bool) {
	// Both stop reasons cut generation off mid-block: max_tokens is the output
	// cap, model_context_window_exceeded is the context limit (Claude 4.5+
	// accepts input+max_tokens>context and stops this way rather than 400ing).
	if len(m.Content) == 0 {
		return "", false
	}
	switch m.StopReason {
	case "max_tokens", "model_context_window_exceeded":
	default:
		return "", false
	}
	last := m.Content[len(m.Content)-1]
	if last.Type != "tool_use" || !isEmptyToolInput(last.Input) {
		return "", false
	}
	return last.ID, true
}

func isEmptyToolInput(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "{}", "null":
		return true
	}
	return false
}

// truncatedToolNote is what a cut-off tool call returns instead of running.
//
// It names where the limit came from because two of the sources mean the
// advice "produce less" is the wrong fix. Under a config override or the
// unknown-model floor the cap is ours, not the model's, and raising it is the
// real remedy — the Opus 5.5 floor bug showed only "max_tokens=8192", which
// read as the model's limit and sent the model splitting files it could have
// written whole. The other sources are the model's real cap, so they get the
// label and no hint.
func truncatedToolNote(maxTokens int64, source string, hints api.Hints) string {
	limit := fmt.Sprintf("max_tokens=%d", maxTokens)
	if source != "" {
		limit += ", from " + source
	}
	note := fmt.Sprintf("This tool call was cut off before its arguments were complete: "+
		"the turn reached the output-token limit (%s) mid-call, so it did NOT "+
		"run — nothing was written or changed. Produce less in one turn: write a large file "+
		"in smaller pieces (create it, then append/edit the rest), or shorten the content, "+
		"then continue.", limit)
	setting := hints.MaxTokens
	if setting == "" {
		setting = "the host's output-token setting"
	}
	switch source {
	case api.LimitSourceConfig:
		note += " This limit is set by " + setting + "; if the model allows more, " +
			"tell the user it can be raised there."
	case api.LimitSourceFallback:
		note += " The model id was not recognised, so this is a conservative floor rather than " +
			"the model's real limit; tell the user to set " + setting + "."
	}
	return note
}

func shortCircuit(emit Emitter, tu anthropic.BetaToolUseBlock, msg string) anthropic.BetaContentBlockParamUnion {
	if emit != nil {
		emit(Event{Type: EventToolResult, ToolName: tu.Name, ToolUseID: tu.ID, Content: msg, IsError: true})
	}
	return anthropic.NewBetaToolResultBlock(tu.ID, msg, true)
}

func (l *Loop) dispatchCall(ctx context.Context, tu anthropic.BetaToolUseBlock, opts Options, emit Emitter, reveal func(...string), fs *failureState) anthropic.BetaContentBlockParamUnion {
	raw, _ := json.Marshal(tu.Input)
	// Repair string-encoded scalars ("offset": "180") before anything reads the
	// input, so the gate, the permission check, the breaker key, the hooks and
	// the tool all see one value. Narrow by construction — see schema.Coerce.
	if t, ok := l.tools.Lookup(tu.Name); ok {
		if fixed, changed := schema.Coerce(t.InputSchema(), raw); len(changed) > 0 {
			raw = fixed
			var shown any
			if json.Unmarshal(fixed, &shown) == nil {
				tu.Input = shown // report what will actually run
			}
		}
	}
	if emit != nil {
		emit(Event{Type: EventToolUse, ToolName: tu.Name, ToolUseID: tu.ID, Input: tu.Input})
	}

	key := tu.Name + "\x00" + string(raw)
	rawStr := string(raw)
	errResultKind := func(msg string, kind failureKind) anthropic.BetaContentBlockParamUnion {
		// A refusal by the host gate is a decision, not a malfunction. Its text
		// is identical whatever the command was, so feeding it to the same-shape
		// streak makes two unrelated refused commands look like proof the
		// environment is wedged, and loop-breaker B then latches the tool for
		// the rest of the Run — a latch nothing can clear, because the only
		// reset is a successful execution and B is what prevents one. Count the
		// exact call (breaker A still stops a literal retry) but leave the shape
		// streak out of it.
		//
		// A permission refusal is the same decision-not-malfunction case and is
		// kept out of BOTH counters, because unlike a host-gate refusal it is
		// routinely revoked in the middle of a Run — by an approved
		// ExitPlanMode, or by /mode — through a route the loop never observes,
		// so there is nothing to hang an fs.clear on. See docs/plan-mode.md.
		if kind != failurePermission {
			fs.fail(key)
			if kind != failureHostGate {
				fs.bumpStreak(tu.Name, msg, rawStr, kind == failurePreExec)
			}
		}
		if emit != nil {
			emit(Event{
				Type: EventToolResult, ToolName: tu.Name, ToolUseID: tu.ID,
				Content: msg, IsError: true, HostBlocked: kind == failureHostGate,
			})
		}
		return anthropic.NewBetaToolResultBlock(tu.ID, msg, true)
	}
	// errResult: the tool ran and failed.
	errResult := func(msg string) anthropic.BetaContentBlockParamUnion {
		return errResultKind(msg, failureExec)
	}
	// errBadCall: the call was refused before the tool ran, so no conclusion
	// about the environment may be drawn from a run of these.
	errBadCall := func(msg string) anthropic.BetaContentBlockParamUnion {
		return errResultKind(msg, failurePreExec)
	}
	// errDenied: refused by the permission check (plan mode, or the frontend's
	// Approver). Neither breaker counts it.
	errDenied := func(msg string) anthropic.BetaContentBlockParamUnion {
		return errResultKind(msg, failurePermission)
	}

	// Loop-breaker A: this exact call has already failed repeatedly. Don't run
	// it again — it would fail identically. Refuse with shortCircuit for the
	// same reason B does: we are declining to run the call, not observing it
	// fail, and recording our own refusal as a failure would let A feed B until
	// B latches the tool outright.
	if n := fs.count(key); n >= repeatFailureLimit {
		return shortCircuit(emit, tu, repeatedFailureMsg(tu.Name, n))
	}
	// Loop-breaker B: the same tool has produced the same KIND of error for
	// `repeatFailureLimit` consecutive calls. Two cases — identical inputs (the
	// model is in a guessing loop) or varied inputs (the environment is wedged
	// and the same error shape comes back regardless). Different directives.
	if streak := fs.streak(tu.Name); streak.count >= repeatFailureLimit && streak.blocks(rawStr) {
		// Use shortCircuit() so we don't keep growing the streak on each fired
		// short-circuit (the model isn't actually trying — we're refusing).
		msg := repeatedShapeFailureMsg(tu.Name, streak.count, streak.sig)
		if streak.varied && !streak.preExec {
			msg = envFailureMsg(tu.Name, streak.count, streak.sig)
		}
		// Clear the streak as the directive goes out, because the only other
		// reset is a successful execution and this branch is what prevents one.
		// Left in place it is not a circuit breaker but a latch: a real session
		// reached a state where `true`, `pwd` and `echo hello` were all refused
		// in 0ms for the rest of the run, long after whatever broke had passed.
		// Firing once per streak keeps the anti-loop property — a model that
		// keeps failing gets the directive again after another
		// repeatFailureLimit failures — without making the tool unusable.
		fs.clearStreak(tu.Name)
		return shortCircuit(emit, tu, msg)
	}

	tool, ok := l.tools.Lookup(tu.Name)
	if !ok {
		return errBadCall(l.unknownToolMsg(tu.Name))
	}

	// Validation runs first: before the host gate and before the permission
	// check and its approval prompt. A call with malformed arguments cannot run
	// whatever the gate or the user decides, so asking either about it is
	// wasted work at best and an approval prompt for nothing at worst. It is
	// safe to run ahead of the gate because ValidateInput is pure: it reads the
	// arguments, never the filesystem, so nothing about the machine is learnt
	// from a call the gate would have refused. The order is therefore
	// validation, host gate, permission check, PreToolUse hooks.
	if err := tool.ValidateInput(raw); err != nil {
		msg := fmt.Sprintf("Input validation error: %v", err)
		// Tell the model what the tool actually accepts. Smaller models hallucinate
		// param names ("line_start" instead of "offset") and keep retrying with the
		// same wrong shape — listing the real fields lets them self-correct.
		if fields := schemaFieldList(tool.InputSchema()); fields != "" {
			msg += fmt.Sprintf(" — %s accepts: %s.", tu.Name, fields)
		}
		return errBadCall(msg)
	}

	// The host gate runs BEFORE the permission check, not after. The check is
	// per-tool and mode-driven, and in autonomous — the default — it allows
	// everything, so a gate running second would never see the calls that most
	// need it. See hostgate.go.
	//
	// bypassPermissions skips it, because that mode's entire contract is "no
	// checks" and honouring half of it would be worse than honouring none.
	if opts.Gate != nil && permission.CurrentMode(opts.Permission) != permission.ModeBypassPermissions {
		v := opts.Gate.Check(ctx, GateCall{
			Tool: tu.Name, ToolUseID: tu.ID, Input: raw, WorkingDir: opts.WorkingDir, Approver: opts.Approver,
		})
		if !v.Allow {
			return errResultKind(v.Refuse, failureHostGate)
		}
		if v.Approved {
			// Approval is new information about the tool's prospects. Anything
			// it was carrying from earlier refusals — this call's own count and
			// the tool's shape streak — is now stale, and leaving it in place
			// is how "permission granted" still leaves the tool unusable.
			fs.clear(key, tu.Name)
		}
	}

	req := tool.PermissionRequest(raw)
	decision := permission.Check(opts.Permission, tool, req)
	switch decision.Behavior {
	case permission.Deny:
		msg := decision.Message
		if msg == "" {
			msg = fmt.Sprintf("Permission denied for tool %s", tu.Name)
		}
		return errDenied(msg)
	case permission.Ask:
		// Delegate the decision to the frontend's Approver.
		approver := opts.Approver
		if approver == nil {
			approver = DenyAll
		}
		ad := approver.Approve(ctx, ApprovalRequest{
			ToolName:   tu.Name,
			ToolUseID:  tu.ID,
			Input:      raw,
			Specifier:  req.Specifier,
			Suggestion: decision.Message,
		})
		if ad.Behavior != permission.Allow {
			msg := ad.Message
			if msg == "" {
				msg = fmt.Sprintf("Permission denied for tool %s", tu.Name)
			}
			return errDenied(msg)
		}
	}

	if opts.BeforeEdit != nil {
		if paths := editedPaths(tu.Name, raw); len(paths) > 0 {
			opts.BeforeEdit(tu.Name, paths)
		}
	}

	// PreToolUse. Last, after validation, the gate and the permission check, so
	// a hook is only ever asked about a call Klaudia was otherwise going to
	// make — it can narrow, never widen. See hooks.go.
	//
	// A refusal is classified as a host-gate failure for the same reason the
	// gate's own is: the text is identical every time the hook fires, so feeding
	// it to the same-shape streak would make two unrelated blocked calls look
	// like a wedged environment and latch the tool.
	if hookEnabled(opts, HookPreToolUse) {
		hr := fireHooks(ctx, opts, emit, HookInput{
			Event:     HookPreToolUse,
			ToolName:  tu.Name,
			ToolInput: toolInputFor(raw),
		})
		if hr.Blocked {
			return errResultKind(hookBlockedMsg(tu.Name, hr.Reason), failureHostGate)
		}
	}

	// Progress is only wired when someone is listening. A long-running tool (the
	// Agent tool, which runs a whole child loop) reports through this so the
	// frontend can show movement instead of an unexplained pause.
	var progress func(string)
	if emit != nil {
		progress = func(line string) {
			emit(Event{
				Type:      EventToolProgress,
				ToolName:  tu.Name,
				ToolUseID: tu.ID,
				Text:      line,
			})
		}
	}
	results, err := tool.Execute(ctx, tools.Context{
		WorkingDir:   opts.WorkingDir,
		Ask:          opts.Asker,
		Plan:         opts.Planner,
		Plans:        opts.Plan,
		Reveal:       reveal,
		HostChange:   hostChangeFor(opts),
		Progress:     progress,
		ReadText:     opts.ReadText,
		Spill:        opts.Spill,
		Background:   opts.Background,
		Conversation: opts.forkBase,
	}, raw)
	if err != nil {
		return errResult(fmt.Sprintf("Tool execution error: %v", err))
	}

	// Collapse the result text, and collect any image blocks (vision). `full`
	// tracks the display-only variant in parallel: a tool that clamped its
	// output sets Result.Full, and the UI gets that while the model gets the
	// clamped Content. clamped stays false when no tool did, so the event
	// carries no redundant copy of the same string.
	var data map[string]any
	var content, full string
	isErr := false
	clamped := false
	var images []tools.ResultImage
	for i, r := range results {
		if i > 0 && r.Content != "" {
			content += "\n"
			full += "\n"
		}
		content += r.Content
		full += r.Display()
		if r.Full != "" {
			clamped = true
		}
		isErr = isErr || r.IsError
		images = append(images, r.Images...)
		for k, v := range r.Data {
			if data == nil {
				data = map[string]any{}
			}
			data[k] = v
		}
	}

	// The backstop. Tools that understand their own output clamp it better
	// than this can — Bash keeps a tail because that is where the verdict is —
	// but most tools do not clamp at all, and before this every one of them
	// could put its entire output into the window. A 980 KB Grep result or a
	// chatty MCP server was one call away from an unusable session. Content
	// already within budget comes back untouched, so self-clamping tools are
	// unaffected.
	if capped, cut := opts.Spill.Cap(tu.Name, content); cut {
		content = capped
		clamped = true
	}

	// PostToolUse. After the cap, so a hook's feedback cannot be the part that
	// gets truncated away, and after the result is final so a hook reading the
	// file on disk sees what the model will be told about.
	//
	// A hook here can also refuse, which does not undo anything — the tool has
	// already run. What it buys is the result arriving as an error with the
	// reason attached, which is the difference between the model believing its
	// write succeeded and knowing the linter rejected it.
	if hookEnabled(opts, HookPostToolUse) {
		hr := fireHooks(ctx, opts, emit, HookInput{
			Event:      HookPostToolUse,
			ToolName:   tu.Name,
			ToolInput:  toolInputFor(raw),
			ToolResult: content,
			ToolError:  isErr,
		})
		if hr.Blocked {
			isErr = true
			content = hookFeedback(content, hr.Reason)
		} else {
			content = hookFeedback(content, hr.Context)
		}
	}
	if isErr {
		fs.fail(key)
		fs.bumpStreak(tu.Name, streakSignature(content, rawStr), rawStr, false)
	} else {
		// A clean run resets both counters: this exact call's retry count and
		// the per-tool same-shape streak (the tool clearly isn't fundamentally
		// broken for the caller).
		fs.clear(key, tu.Name)
	}
	if emit != nil {
		ev := Event{Type: EventToolResult, ToolName: tu.Name, ToolUseID: tu.ID, Content: content, IsError: isErr, Data: data}
		if clamped {
			ev.FullContent = full
		}
		emit(ev)
	}
	if len(images) == 0 {
		return anthropic.NewBetaToolResultBlock(tu.ID, content, isErr)
	}
	return toolResultWithImages(tu.ID, content, isErr, images)
}

// toolResultWithImages builds a tool_result block carrying text plus one or
// more base64 image blocks (for Read of image files).
func toolResultWithImages(toolUseID, text string, isErr bool, images []tools.ResultImage) anthropic.BetaContentBlockParamUnion {
	content := make([]anthropic.BetaToolResultBlockParamContentUnion, 0, len(images)+1)
	if text != "" {
		content = append(content, anthropic.BetaToolResultBlockParamContentUnion{
			OfText: &anthropic.BetaTextBlockParam{Text: text},
		})
	}
	for _, img := range images {
		content = append(content, anthropic.BetaToolResultBlockParamContentUnion{
			OfImage: &anthropic.BetaImageBlockParam{
				Source: anthropic.BetaImageBlockParamSourceUnion{
					OfBase64: &anthropic.BetaBase64ImageSourceParam{
						Data:      img.Base64,
						MediaType: anthropic.BetaBase64ImageSourceMediaType(img.MediaType),
					},
				},
			},
		})
	}
	return anthropic.BetaContentBlockParamUnion{
		OfToolResult: &anthropic.BetaToolResultBlockParam{
			ToolUseID: toolUseID,
			IsError:   anthropic.Bool(isErr),
			Content:   content,
		},
	}
}

// buildToolParams converts the registry's tools into API tool params. Deferred
// tools are omitted unless they've been revealed (ToolSearch); ToolSearch itself
// is always included so the model can discover the deferred ones.
func (l *Loop) buildToolParams(ctx context.Context, deferred, revealed map[string]bool, canAsk bool) ([]anthropic.BetaToolUnionParam, error) {
	names := l.tools.Names()
	out := make([]anthropic.BetaToolUnionParam, 0, len(names))
	for _, name := range names {
		if deferred[name] && !revealed[name] && name != "ToolSearch" {
			continue
		}
		t, _ := l.tools.Lookup(name)
		// Nobody to ask: leave out a tool that can only ask (tools.AskOnly),
		// rather than offer one whose every call fails. (PR #13 raised it.)
		if _, askOnly := t.(tools.AskOnly); askOnly && !canAsk {
			continue
		}
		desc, err := t.Description(ctx)
		if err != nil {
			return nil, fmt.Errorf("tool %s description: %w", name, err)
		}
		out = append(out, anthropic.BetaToolUnionParam{
			OfTool: &anthropic.BetaToolParam{
				Name:        t.Name(),
				Description: anthropic.String(desc),
				InputSchema: inputSchemaParam(t.InputSchema()),
			},
		})
	}
	return out, nil
}

// schemaFieldList renders a tool's accepted input fields as
// "file_path (required), offset, limit" — appended to validation errors so the
// model sees what's correct, not just what was wrong. Returns "" when the
// schema has no properties (e.g. parameterless tools).
func schemaFieldList(raw json.RawMessage) string {
	props, required := splitSchema(raw)
	pm, ok := props.(map[string]any)
	if !ok || len(pm) == 0 {
		return ""
	}
	req := make(map[string]bool, len(required))
	for _, r := range required {
		req[r] = true
	}
	names := make([]string, 0, len(pm))
	for k := range pm {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		if req[n] {
			parts[i] = n + " (required)"
		} else {
			parts[i] = n
		}
	}
	return strings.Join(parts, ", ")
}

// structuralKeys are the top-level schema keywords that must travel with
// properties for the schema to mean anything: the definitions a "$ref"
// points into, and top-level unions. MCP servers built on Pydantic or zod
// emit "$ref": "#/$defs/X", and sending properties alone left every such ref
// dangling. additionalProperties is deliberately not here: every built-in
// tool declares it, and dropping it only ever loosened a schema, so carrying
// it would change every request for no fix.
var structuralKeys = []string{"$defs", "definitions", "anyOf", "oneOf", "allOf"}

// inputSchemaParam turns a tool's JSON Schema into the API's input_schema:
// properties and required, plus the structural keywords above.
func inputSchemaParam(raw json.RawMessage) anthropic.BetaToolInputSchemaParam {
	props, required := splitSchema(raw)
	p := anthropic.BetaToolInputSchemaParam{Properties: props, Required: required}
	var all map[string]json.RawMessage
	if json.Unmarshal(raw, &all) == nil {
		for _, k := range structuralKeys {
			if v, ok := all[k]; ok {
				if p.ExtraFields == nil {
					p.ExtraFields = map[string]any{}
				}
				p.ExtraFields[k] = v
			}
		}
	}
	return p
}

// splitSchema pulls "properties" and "required" out of a generated JSON Schema
// object so they can be placed into BetaToolInputSchemaParam.
func splitSchema(raw json.RawMessage) (properties any, required []string) {
	var s struct {
		Properties json.RawMessage `json:"properties"`
		Required   []string        `json:"required"`
	}
	_ = json.Unmarshal(raw, &s)
	if len(s.Properties) > 0 {
		var p any
		_ = json.Unmarshal(s.Properties, &p)
		properties = p
	}
	return properties, s.Required
}

// record marshals a message param and hands it to the recorder, returning
// the recorder's error.
func record(r Recorder, role string, msg any) error {
	if r == nil {
		return nil
	}
	// A response BetaMessage must be converted to its request param before it
	// is marshalled. The SDK's *response* union types carry no MarshalJSON, so
	// json.Marshal on one dumps every internal field of the union — a text
	// block comes out with "OfBetaWebSearchResultBlockArray" and a dozen other
	// empty fields alongside the real one. For plain text and tool_use that is
	// merely ugly, but for a server-tool result (web_search / web_fetch) the
	// content field serialises to an object the API rejects with
	// "content ... Input should be a valid array" the next time the history is
	// sent — which is every resume. ToParam() runs through the param
	// marshallers, which is clean and round-trips. RawJSON() is not an
	// alternative: after streaming accumulation the SDK sets it to this same
	// leaky marshal, so it is corrupt too. Params (user and tool_result
	// messages) already marshal correctly and pass straight through.
	if m, ok := msg.(anthropic.BetaMessage); ok {
		msg = m.ToParam()
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return r.Record(role, b)
}

// historyParam is the assistant turn as it goes into the running history:
// ToParam() and then a JSON round-trip, so the live conversation holds exactly
// what a resumed one rebuilds from the transcript.
//
// ToParam() alone is not that. For a web_fetch result it keeps the content in
// a form that marshals correctly but leaves OfRequestWebFetchResultBlock nil,
// so brokenResultToolUseID read a healthy fetch as corrupt. The repair then
// dropped the fetch, and the turn it arrived in, which carries thinking, went
// with it. That was the "an earlier web fetch was interrupted" notice in a
// session that had fetched successfully, and with the turn gone the API
// delivered the same fetch again and the model re-asked the questions the
// user had already answered (measured: one fetch delivered in 9 assistant
// messages of a goal-setting session). Replaying the transcript never showed
// it, because the replay path decodes from JSON.
func historyParam(m anthropic.BetaMessage) anthropic.BetaMessageParam {
	p := m.ToParam()
	b, err := json.Marshal(p)
	if err != nil {
		return p
	}
	var out anthropic.BetaMessageParam
	if err := json.Unmarshal(b, &out); err != nil {
		return p
	}
	return out
}

// toolUseBlocks returns the tool_use blocks in an assistant message.
func toolUseBlocks(m anthropic.BetaMessage) []anthropic.BetaToolUseBlock {
	var out []anthropic.BetaToolUseBlock
	for _, b := range m.Content {
		if b.Type == "tool_use" {
			out = append(out, b.AsToolUse())
		}
	}
	return out
}

// finalAssistantText concatenates the text blocks of an assistant message.
func finalAssistantText(m anthropic.BetaMessage) string {
	var s string
	for _, b := range m.Content {
		if b.Type == "text" {
			s += b.AsText().Text
		}
	}
	return s
}

// hostChangeFor returns the declaration tool's approver for a run, or nil when
// there is no gate to declare against. Nil is meaningful: the tool tells the
// model there is no one to ask, rather than reporting a failure it cannot act
// on.
func hostChangeFor(opts Options) tools.HostApprover {
	if opts.Gate == nil {
		return nil
	}
	return opts.Gate.HostChanges(opts.Approver)
}

// maxTokensAdvice is the parenthetical on the retried-at-the-limit notice: how
// to stop paying for the round-trip, when the host has said where its setting
// lives.
func maxTokensAdvice(h api.Hints) string {
	if h.MaxTokens == "" {
		return ""
	}
	return " (set " + h.MaxTokens + " to avoid the wasted round-trip)"
}
