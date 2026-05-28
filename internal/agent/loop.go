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

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/compaction"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// defaultMaxTokens is the per-response output cap when the caller doesn't set one.
const defaultMaxTokens = 8192

// Emitter receives streaming events for stream-json output. It is called for
// assistant text, tool_use, and tool_result events as they occur. May be nil.
type Emitter func(event Event)

// Event is a streaming event emitted during a run (stream-json mode).
type Event struct {
	Type      string `json:"type"`                  // "assistant" | "tool_use" | "tool_result"
	Text      string `json:"text,omitempty"`        // assistant text
	ToolName  string `json:"tool_name,omitempty"`   // tool_use / tool_result
	ToolUseID string `json:"tool_use_id,omitempty"` // tool_use / tool_result
	Input     any    `json:"input,omitempty"`       // tool_use input
	Content   string `json:"content,omitempty"`     // tool_result content
	IsError   bool   `json:"is_error,omitempty"`    // tool_result error flag
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
	MaxTokens  int64 // 0 = defaultMaxTokens
	Permission permission.Context
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
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
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

	// failures tracks how many times each identical tool call (name+input) has
	// failed within this Run, so dispatch can break a model out of a retry loop.
	failures := map[string]int{}

	messages := append([]anthropic.BetaMessageParam{}, opts.InitialMessages...)
	if opts.Prompt != "" {
		userMsg := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(opts.Prompt))
		messages = append(messages, userMsg)
		record(opts.Recorder, "user", userMsg)
	}

	var res Result
	for {
		res.NumTurns++

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
		res.Text = finalText

		// Add the assistant turn to the running conversation. Persisting to the
		// transcript is DEFERRED until we know it's either final (no tools) or
		// has its tool_result paired up — see the record calls below. Persisting
		// here, then dying mid-dispatch (Ctrl+C / SIGTERM / OOM), would leak an
		// orphan tool_use to disk and poison the next resume.
		messages = append(messages, assistant.ToParam())

		// pause_turn: the API paused a long-running server-side tool (e.g. web
		// search). Re-send the accumulated turn to let it continue. The
		// assistant has no local tool_use to pair, so record it now.
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
			return res, nil
		}

		// Dispatch tools and append their results. reveal lets ToolSearch mark
		// deferred tools active for subsequent turns.
		reveal := func(names ...string) {
			for _, n := range names {
				revealed[n] = true
			}
		}
		resultBlocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			block := l.dispatch(ctx, tu, opts, emit, reveal, failures)
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
	return fmt.Sprintf("No such tool available: %s", name)
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

func (l *Loop) dispatch(ctx context.Context, tu anthropic.BetaToolUseBlock, opts Options, emit Emitter, reveal func(...string), failures map[string]int) anthropic.BetaContentBlockParamUnion {
	raw, _ := json.Marshal(tu.Input)
	if emit != nil {
		emit(Event{Type: "tool_use", ToolName: tu.Name, ToolUseID: tu.ID, Input: tu.Input})
	}

	key := tu.Name + "\x00" + string(raw)
	errResult := func(msg string) anthropic.BetaContentBlockParamUnion {
		failures[key]++
		if emit != nil {
			emit(Event{Type: "tool_result", ToolName: tu.Name, ToolUseID: tu.ID, Content: msg, IsError: true})
		}
		return anthropic.NewBetaToolResultBlock(tu.ID, msg, true)
	}

	// Loop-breaker: this exact call has already failed repeatedly. Don't run it
	// again (it would fail identically); return directive steering instead so a
	// model stuck in a retry loop gets a clear push to do something different.
	if failures[key] >= repeatFailureLimit {
		return errResult(repeatedFailureMsg(tu.Name, failures[key]))
	}

	tool, ok := l.tools.Lookup(tu.Name)
	if !ok {
		return errResult(l.unknownToolMsg(tu.Name))
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
		return errResult(fmt.Sprintf("Input validation error: %v", err))
	}

	results, err := tool.Execute(ctx, tools.Context{Ask: opts.Asker, Plan: opts.Planner, Reveal: reveal}, raw)
	if err != nil {
		return errResult(fmt.Sprintf("Tool execution error: %v", err))
	}

	// Collapse the result text, and collect any image blocks (vision).
	var content string
	isErr := false
	var images []tools.ResultImage
	for i, r := range results {
		if i > 0 && r.Content != "" {
			content += "\n"
		}
		content += r.Content
		isErr = isErr || r.IsError
		images = append(images, r.Images...)
	}
	if isErr {
		failures[key]++
	} else {
		delete(failures, key) // a clean run clears the retry counter for this call
	}
	if emit != nil {
		emit(Event{Type: "tool_result", ToolName: tu.Name, ToolUseID: tu.ID, Content: content, IsError: isErr})
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
