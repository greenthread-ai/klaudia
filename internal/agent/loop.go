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
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/compaction"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// Emitter receives streaming events for stream-json output. It is called for
// assistant text, tool_use, and tool_result events as they occur. May be nil.
type Emitter func(event Event)

// Event is a streaming event emitted during a run (stream-json mode).
type Event struct {
	Type      string `json:"type"`                  // "assistant" | "tool_use" | "tool_result" | "usage" | "compaction"
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
	// Usage deltas for one inner-loop LLM call, emitted after each streamTurn so
	// the TUI/stream-json frontend can update token counters live during long
	// goal iterations rather than waiting for the whole Run to return. The
	// matching Result fields (NumTurns/InputTokens/OutputTokens) stay
	// authoritative — the TUI reconciles against them at doneMsg so dropped
	// usage events still settle correctly.
	InputDelta  int64 `json:"input_delta,omitempty"`
	OutputDelta int64 `json:"output_delta,omitempty"`
	TurnDelta   int   `json:"turn_delta,omitempty"`
}

// Recorder persists conversation messages to a transcript as the loop runs.
// role is "user" or "assistant"; message is the raw Anthropic message JSON.
type Recorder interface {
	Record(role string, message json.RawMessage) error
}

// Options configures a single Run.
type Options struct {
	Prompt     string
	Model      anthropic.Model
	System     string
	MaxTurns   int   // 0 = unlimited
	MaxTokens  int64 // 0 = model-aware default via api.MaxOutputTokens
	Permission permission.Context
	// Host is the trust gate: it classifies each tool call and refuses changes
	// to this machine that the user has not agreed to. Nil disables the whole
	// feature, which is what every caller that has not been wired up yet gets.
	Host *HostGate
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
	// DeferredTools names tools withheld from the initial request (loaded on
	// demand once ToolSearch reveals them). Typically the MCP tools.
	DeferredTools map[string]bool
	// ContextWindow is the model's context size, used for autocompact
	// thresholds. 0 uses the package default.
	ContextWindow int
	// PartialMessages, if set, receives raw model stream events during the main
	// answer turn (not compaction summaries). The CLI wires this to a
	// stream_event emitter when --include-partial-messages is set. Nil by
	// default and for the TUI, so its single-reader invariant is untouched.
	PartialMessages func(anthropic.BetaRawMessageStreamEventUnion)
	// OnSummary, if set, is called with each compaction summary the loop
	// produces (autocompact). The CLI persists it alongside the transcript for
	// token-saving resume. May be nil.
	OnSummary func(summary string)
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
}

// New builds a Loop over a model provider (Anthropic, OpenAI-compatible, …).
func New(provider api.Provider, registry *tools.Registry) *Loop {
	return &Loop{provider: provider, tools: registry}
}

// Run executes the loop until the model stops calling tools or MaxTurns is hit.
func (l *Loop) Run(ctx context.Context, opts Options, emit Emitter) (Result, error) {
	maxTokens := opts.MaxTokens
	if maxTokens <= 0 {
		// Model-aware default: the flat 8192 was low enough that an ordinary
		// large write hit the output limit mid-tool-call and was dispatched with
		// empty arguments. api.MaxOutputTokens raises it on models that support
		// more, staying a safe under-approximation of each model's real cap.
		maxTokens = int64(api.MaxOutputTokens(string(opts.Model)))
	}

	// Deferred tools are withheld from the request until ToolSearch reveals them.
	betas := api.DefaultBetas
	if opts.WebTools {
		betas = append(append([]string{}, betas...), api.WebToolBetas...)
	}
	revealed := map[string]bool{}

	var system []anthropic.BetaTextBlockParam
	if opts.System != "" {
		system = []anthropic.BetaTextBlockParam{{Text: opts.System}}
	}

	// failures tracks how many times each IDENTICAL tool call (name+input) has
	// failed within this Run; errStreaks tracks how many consecutive failures
	// of the same SHAPE a tool has produced (regardless of input). Together
	// they break a model out of retry loops where either the same call or the
	// same kind of mistake keeps recurring.
	failures := map[string]int{}
	errStreaks := map[string]errStreak{}

	messages := append([]anthropic.BetaMessageParam{}, opts.InitialMessages...)
	if opts.Prompt != "" {
		userMsg := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(opts.Prompt))
		messages = append(messages, userMsg)
		record(opts.Recorder, "user", userMsg)
	}

	var res Result
	halted := false
	for {
		res.NumTurns++

		// A correction the user typed mid-turn goes in before the request is
		// built, so the model sees it while deciding what to do next rather
		// than after it has done it. See steer.go.
		if in := pollInterjection(opts); !in.Empty() {
			if msg, ok := steerMessage(in); ok {
				messages = append(messages, msg)
				record(opts.Recorder, "user", msg)
				if emit != nil {
					emit(Event{Type: "steer", Content: in.Text})
				}
			}
			halted = halted || in.Halt
		}

		// Compaction runs at the top of every turn (docs/compaction.md):
		// microcompact first (cheap, local), then autocompact (model-based) if
		// near the context limit.
		messages = l.compact(ctx, messages, opts, emit)

		// Build the tool list for this turn: eager tools plus any deferred tools
		// revealed so far (via ToolSearch). Rebuilt per turn so reveals take
		// effect on the next request.
		toolParams, terr := l.buildToolParams(ctx, opts.DeferredTools, revealed)
		if terr != nil {
			res.Messages = messages
			return res, terr
		}
		if opts.WebTools {
			toolParams = append(toolParams, webToolParams()...)
		}

		// Repair any message with empty content (e.g. an old refusal recorded with
		// content: null) before sending — the Anthropic API otherwise rejects the
		// whole request with "messages.<i>.content: Field required".
		params := anthropic.BetaMessageNewParams{
			Model:     opts.Model,
			MaxTokens: maxTokens,
			Messages:  sanitizeMessages(messages),
			System:    system,
			Tools:     toolParams,
			Betas:     betas,
		}

		assistant, finalText, err := l.streamTurn(ctx, params, emit, opts.PartialMessages)
		if err != nil {
			res.Messages = messages
			return res, err
		}
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
				Type:        "usage",
				InputDelta:  assistant.Usage.InputTokens,
				OutputDelta: assistant.Usage.OutputTokens,
				TurnDelta:   1,
			})
		}

		// Add the assistant turn to the running conversation. Persisting to the
		// transcript is DEFERRED until we know it's either final (no tools) or
		// has its tool_result paired up — see the record calls below. Persisting
		// here, then dying mid-dispatch (Ctrl+C / SIGTERM / OOM), would leak an
		// orphan tool_use to disk and poison the next resume.
		messages = append(messages, assistant.ToParam())

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
			record(opts.Recorder, "assistant", assistant)
			continue
		}

		// Collect tool_use blocks from this turn.
		toolUses := toolUseBlocks(assistant)
		if len(toolUses) == 0 {
			// Final (tool-less) answer: structurally fine on its own, record now.
			record(opts.Recorder, "assistant", assistant)
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
			record(opts.Recorder, "assistant", assistant)
			res.StopReason = "user_halt"
			res.Messages = messages
			return res, nil
		}

		// Dispatch tools and append their results. reveal lets ToolSearch mark
		// deferred tools active for subsequent turns.
		reveal := func(names ...string) {
			for _, n := range names {
				revealed[n] = true
			}
		}
		// A tool_use the model never finished emitting because the turn hit the
		// output-token limit arrives here with empty ("{}") arguments — the
		// stream layer patches its truncated, invalid JSON so Accumulate doesn't
		// crash (see api.repairInvalidToolInputs). Dispatching it would run the
		// tool with no arguments (e.g. Write with no file_path/content, which the
		// user saw as a cryptic schema error and a retry loop). Return an
		// actionable result instead so the model shortens or continues.
		truncID, wasTruncated := truncatedToolUseID(assistant)
		resultBlocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			if wasTruncated && tu.ID == truncID {
				resultBlocks = append(resultBlocks, shortCircuit(emit, tu, truncatedToolNote(maxTokens)))
				continue
			}
			block := l.dispatch(ctx, tu, opts, emit, reveal, failures, errStreaks)
			resultBlocks = append(resultBlocks, block)
		}
		toolResultMsg := anthropic.NewBetaUserMessage(resultBlocks...)
		messages = append(messages, toolResultMsg)

		// Now persist BOTH back-to-back. The window for process termination
		// between them is microseconds (two Recorder.Record calls), whereas the
		// dispatch above can be seconds-to-minutes — that gap is where orphans
		// used to land in the transcript.
		record(opts.Recorder, "assistant", assistant)
		record(opts.Recorder, "user", toolResultMsg)

		// The second poll point. Between the tool results and the next request
		// is the other place a user message can be appended without splitting a
		// tool_use/tool_result pair — and it is the one that matters, because a
		// long turn is mostly tool batches, not turn boundaries.
		if in := pollInterjection(opts); !in.Empty() {
			if msg, ok := steerMessage(in); ok {
				messages = append(messages, msg)
				record(opts.Recorder, "user", msg)
				if emit != nil {
					emit(Event{Type: "steer", Content: in.Text})
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

// compact applies microcompact then (if near the limit) autocompact to the
// message list. Honors DISABLE_COMPACT / DISABLE_MICROCOMPACT /
// DISABLE_AUTO_COMPACT, matching the JS env switches.
func (l *Loop) compact(ctx context.Context, messages []anthropic.BetaMessageParam, opts Options, emit Emitter) []anthropic.BetaMessageParam {
	if os.Getenv("DISABLE_COMPACT") != "" {
		return messages
	}

	if os.Getenv("DISABLE_MICROCOMPACT") == "" {
		if out, res := compaction.Microcompact(messages); res.Compacted {
			messages = out
			if emit != nil {
				emit(Event{Type: "compaction", Content: fmt.Sprintf("microcompact: elided %d old tool results (~%d tokens saved)", res.ElidedCount, res.TokensSaved)})
			}
		}
	}

	if os.Getenv("DISABLE_AUTO_COMPACT") == "" &&
		compaction.ShouldAutocompact(compaction.EstimateTokens(messages), opts.ContextWindow) {
		// Autocompact is a model call that runs silently (emit=nil), so signal
		// its start with a contentless "compaction" event — the frontend shows
		// "compacting…" for the duration. Microcompact above is instant and
		// skips this: it only drops the done banner below.
		if emit != nil {
			emit(Event{Type: "compaction"})
		}
		if out, ok := l.autocompact(ctx, messages, opts); ok {
			messages = out
			if emit != nil {
				emit(Event{Type: "compaction", Content: "autocompact: summarized prior conversation"})
			}
		}
	}
	return messages
}

// Compact unconditionally summarizes the conversation via the model and returns
// the replacement history plus the summary text. Used by the TUI's /compact
// command (the loop's own autocompact runs automatically near the context
// limit). Returns an error if the summary call fails or yields no text.
func (l *Loop) Compact(ctx context.Context, messages []anthropic.BetaMessageParam, model anthropic.Model) ([]anthropic.BetaMessageParam, string, error) {
	req := compaction.BuildSummaryRequest(messages, model, 4096)
	req.Betas = api.DefaultBetas
	assistant, _, err := l.streamTurn(ctx, req, nil, nil)
	if err != nil {
		return nil, "", err
	}
	summary := finalAssistantText(assistant)
	if summary == "" {
		return nil, "", fmt.Errorf("compaction produced no summary")
	}
	return compaction.ReplaceWithSummary(summary), summary, nil
}

// autocompact summarizes the conversation via the model and replaces history
// with the summary. Returns (messages, false) if the summary call fails.
func (l *Loop) autocompact(ctx context.Context, messages []anthropic.BetaMessageParam, opts Options) ([]anthropic.BetaMessageParam, bool) {
	req := compaction.BuildSummaryRequest(messages, opts.Model, 4096)
	req.Betas = api.DefaultBetas
	assistant, _, err := l.streamTurn(ctx, req, nil, nil)
	if err != nil {
		return messages, false
	}
	summary := finalAssistantText(assistant)
	if summary == "" {
		return messages, false
	}
	if opts.OnSummary != nil {
		opts.OnSummary(summary)
	}
	return compaction.ReplaceWithSummary(summary), true
}

// streamTurn issues one streaming request via the provider, emitting
// assistant-text events as deltas arrive, and returns the assembled message.
// rawSink, if non-nil, receives the raw stream events for partial-message
// output; it is nil for compaction summary turns.
func (l *Loop) streamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, emit Emitter, rawSink func(anthropic.BetaRawMessageStreamEventUnion)) (anthropic.BetaMessage, string, error) {
	sink := api.StreamSink{
		OnText: func(delta string) {
			if emit != nil {
				emit(Event{Type: "assistant", Text: delta})
			}
		},
		OnRawEvent: rawSink,
	}
	assistant, err := l.provider.StreamTurn(ctx, params, sink)
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
// across the failing calls. If it did, the failure is almost certainly
// environmental (shell wedged, network down, disk full) rather than a
// tool-input bug — the model is varying its guesses in good faith and still
// hitting the same wall. shortCircuit uses the flag to choose between two
// directive messages.
type errStreak struct {
	sig        string // last error message text
	count      int    // consecutive occurrences of sig
	firstInput string // raw JSON of the input on the first failure in this streak
	varied     bool   // true once a same-sig failure arrived with a different input
}

// bumpErrStreak records a new failure for tool. If the message matches the
// prior signature, the count grows and the input-varied flag tracks whether
// the model is changing inputs across calls. Otherwise the streak starts over.
func bumpErrStreak(streaks map[string]errStreak, tool, msg, input string) {
	prev := streaks[tool]
	if prev.sig == msg {
		prev.count++
		if input != prev.firstInput {
			prev.varied = true
		}
	} else {
		prev = errStreak{sig: msg, count: 1, firstInput: input}
	}
	streaks[tool] = prev
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

// envFailureMsg is the directive returned when loop-breaker B fires but the
// model HAD varied its inputs across the failing calls. The error shape is
// stable while the inputs aren't — the env, not the call, is broken. Telling
// the model to "stop guessing" in this case is actively misleading (it WAS
// trying different things) and pushes it into useless workaround loops.
// Suggest concrete recovery moves instead.
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

func truncatedToolNote(maxTokens int64) string {
	return fmt.Sprintf("This tool call was cut off before its arguments were complete: "+
		"the turn reached the output-token limit (max_tokens=%d) mid-call, so it did NOT "+
		"run — nothing was written or changed. Produce less in one turn: write a large file "+
		"in smaller pieces (create it, then append/edit the rest), or shorten the content, "+
		"then continue.", maxTokens)
}

func shortCircuit(emit Emitter, tu anthropic.BetaToolUseBlock, msg string) anthropic.BetaContentBlockParamUnion {
	if emit != nil {
		emit(Event{Type: "tool_result", ToolName: tu.Name, ToolUseID: tu.ID, Content: msg, IsError: true})
	}
	return anthropic.NewBetaToolResultBlock(tu.ID, msg, true)
}

func (l *Loop) dispatch(ctx context.Context, tu anthropic.BetaToolUseBlock, opts Options, emit Emitter, reveal func(...string), failures map[string]int, errStreaks map[string]errStreak) anthropic.BetaContentBlockParamUnion {
	raw, _ := json.Marshal(tu.Input)
	if emit != nil {
		emit(Event{Type: "tool_use", ToolName: tu.Name, ToolUseID: tu.ID, Input: tu.Input})
	}

	key := tu.Name + "\x00" + string(raw)
	rawStr := string(raw)
	errResultTagged := func(msg string, hostBlocked bool) anthropic.BetaContentBlockParamUnion {
		failures[key]++
		bumpErrStreak(errStreaks, tu.Name, msg, rawStr)
		if emit != nil {
			emit(Event{
				Type: "tool_result", ToolName: tu.Name, ToolUseID: tu.ID,
				Content: msg, IsError: true, HostBlocked: hostBlocked,
			})
		}
		return anthropic.NewBetaToolResultBlock(tu.ID, msg, true)
	}
	errResult := func(msg string) anthropic.BetaContentBlockParamUnion {
		return errResultTagged(msg, false)
	}

	// Loop-breaker A: this exact call has already failed repeatedly. Don't run
	// it again — it would fail identically.
	if failures[key] >= repeatFailureLimit {
		return errResult(repeatedFailureMsg(tu.Name, failures[key]))
	}
	// Loop-breaker B: the same tool has produced the same KIND of error for
	// `repeatFailureLimit` consecutive calls. Two cases — identical inputs (the
	// model is in a guessing loop) or varied inputs (the environment is wedged
	// and the same error shape comes back regardless). Different directives.
	if streak := errStreaks[tu.Name]; streak.count >= repeatFailureLimit {
		// Use shortCircuit() so we don't keep growing the streak on each fired
		// short-circuit (the model isn't actually trying — we're refusing).
		msg := repeatedShapeFailureMsg(tu.Name, streak.count, streak.sig)
		if streak.varied {
			msg = envFailureMsg(tu.Name, streak.count, streak.sig)
		}
		return shortCircuit(emit, tu, msg)
	}

	tool, ok := l.tools.Lookup(tu.Name)
	if !ok {
		return errResult(l.unknownToolMsg(tu.Name))
	}

	// The host gate runs BEFORE the allow/deny rules, not after. An allow rule
	// says a command prefix is fine; it does not say that anything sharing that
	// prefix may change the machine. Checking rules first would let one launder
	// the other. See hostgate.go.
	//
	// bypassPermissions skips it, because that mode's entire contract is "no
	// checks" and honouring half of it would be worse than honouring none.
	if permission.CurrentMode(opts.Permission) != permission.ModeBypassPermissions {
		hd := opts.Host.Check(tu.Name, raw, opts.WorkingDir)
		switch {
		case hd.Refuse != "":
			return errResultTagged(hd.Refuse, true)
		case len(hd.Ask) > 0:
			if !l.approveHostChange(ctx, tu, raw, opts, hd) {
				return errResultTagged(hostDeclinedMsg(hd), true)
			}
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
		return errResult(msg)
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
			return errResult(msg)
		}
	}

	if err := tool.ValidateInput(raw); err != nil {
		msg := fmt.Sprintf("Input validation error: %v", err)
		// Tell the model what the tool actually accepts. Smaller models hallucinate
		// param names ("line_start" instead of "offset") and keep retrying with the
		// same wrong shape — listing the real fields lets them self-correct.
		if fields := schemaFieldList(tool.InputSchema()); fields != "" {
			msg += fmt.Sprintf(" — %s accepts: %s.", tu.Name, fields)
		}
		return errResult(msg)
	}

	if opts.BeforeEdit != nil {
		if paths := editedPaths(tu.Name, raw); len(paths) > 0 {
			opts.BeforeEdit(tu.Name, paths)
		}
	}

	results, err := tool.Execute(ctx, tools.Context{
		WorkingDir: opts.WorkingDir,
		Ask:        opts.Asker,
		Plan:       opts.Planner,
		Reveal:     reveal,
		HostChange: hostChangeFor(opts),
	}, raw)
	if err != nil {
		return errResult(fmt.Sprintf("Tool execution error: %v", err))
	}

	// Collapse the result text, and collect any image blocks (vision). `full`
	// tracks the display-only variant in parallel: a tool that clamped its
	// output sets Result.Full, and the UI gets that while the model gets the
	// clamped Content. clamped stays false when no tool did, so the event
	// carries no redundant copy of the same string.
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
	}
	if isErr {
		failures[key]++
		bumpErrStreak(errStreaks, tu.Name, content, rawStr)
	} else {
		// A clean run resets both counters: this exact call's retry count and
		// the per-tool same-shape streak (the tool clearly isn't fundamentally
		// broken for the caller).
		delete(failures, key)
		delete(errStreaks, tu.Name)
	}
	if emit != nil {
		ev := Event{Type: "tool_result", ToolName: tu.Name, ToolUseID: tu.ID, Content: content, IsError: isErr}
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
func (l *Loop) buildToolParams(ctx context.Context, deferred, revealed map[string]bool) ([]anthropic.BetaToolUnionParam, error) {
	names := l.tools.Names()
	out := make([]anthropic.BetaToolUnionParam, 0, len(names))
	for _, name := range names {
		if deferred[name] && !revealed[name] && name != "ToolSearch" {
			continue
		}
		t, _ := l.tools.Lookup(name)
		desc, err := t.Description(ctx)
		if err != nil {
			return nil, fmt.Errorf("tool %s description: %w", name, err)
		}
		props, required := splitSchema(t.InputSchema())
		out = append(out, anthropic.BetaToolUnionParam{
			OfTool: &anthropic.BetaToolParam{
				Name:        t.Name(),
				Description: anthropic.String(desc),
				InputSchema: anthropic.BetaToolInputSchemaParam{
					Properties: props,
					Required:   required,
				},
			},
		})
	}
	return out, nil
}

// splitSchema pulls "properties" and "required" out of a generated JSON Schema
// object so they can be placed into BetaToolInputSchemaParam.
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

// record marshals a message param and hands it to the recorder (best effort).
func record(r Recorder, role string, msg any) {
	if r == nil {
		return
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
	if b, err := json.Marshal(msg); err == nil {
		_ = r.Record(role, b)
	}
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
