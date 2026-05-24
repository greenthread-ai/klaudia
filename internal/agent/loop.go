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

	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/compaction"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/tools"
)

// defaultMaxTokens is the per-response output cap when the caller doesn't set one.
const defaultMaxTokens = 8192

// Emitter receives streaming events for stream-json output. It is called for
// assistant text, tool_use, and tool_result events as they occur. May be nil.
type Emitter func(event Event)

// Event is a streaming event emitted during a run (stream-json mode).
type Event struct {
	Type       string `json:"type"`                  // "assistant" | "tool_use" | "tool_result"
	Text       string `json:"text,omitempty"`        // assistant text
	ToolName   string `json:"tool_name,omitempty"`   // tool_use / tool_result
	ToolUseID  string `json:"tool_use_id,omitempty"` // tool_use / tool_result
	Input      any    `json:"input,omitempty"`       // tool_use input
	Content    string `json:"content,omitempty"`     // tool_result content
	IsError    bool   `json:"is_error,omitempty"`    // tool_result error flag
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
	// ContextWindow is the model's context size, used for autocompact
	// thresholds. 0 uses the package default.
	ContextWindow int
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
	client *api.Client
	tools  *tools.Registry
}

// New builds a Loop.
func New(client *api.Client, registry *tools.Registry) *Loop {
	return &Loop{client: client, tools: registry}
}

// Run executes the loop until the model stops calling tools or MaxTurns is hit.
func (l *Loop) Run(ctx context.Context, opts Options, emit Emitter) (Result, error) {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}

	toolParams, err := l.buildToolParams(ctx)
	if err != nil {
		return Result{}, err
	}

	// Server-side web tools are appended to the tool list and executed by the
	// API; they require their own betas.
	betas := api.DefaultBetas
	if opts.WebTools {
		toolParams = append(toolParams, webToolParams()...)
		betas = append(append([]string{}, betas...), api.WebToolBetas...)
	}

	var system []anthropic.BetaTextBlockParam
	if opts.System != "" {
		system = []anthropic.BetaTextBlockParam{{Text: opts.System}}
	}

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

		params := anthropic.BetaMessageNewParams{
			Model:     opts.Model,
			MaxTokens: maxTokens,
			Messages:  messages,
			System:    system,
			Tools:     toolParams,
			Betas:     betas,
		}

		assistant, finalText, err := l.streamTurn(ctx, params, emit)
		if err != nil {
			res.Messages = messages
			return res, err
		}
		res.StopReason = string(assistant.StopReason)
		res.InputTokens += assistant.Usage.InputTokens
		res.OutputTokens += assistant.Usage.OutputTokens
		res.Text = finalText

		// Persist the assistant turn (including the final, tool-less answer) and
		// add it to the running conversation.
		record(opts.Recorder, "assistant", assistant)
		messages = append(messages, assistant.ToParam())

		// pause_turn: the API paused a long-running server-side tool (e.g. web
		// search). Re-send the accumulated turn to let it continue.
		if assistant.StopReason == "pause_turn" {
			continue
		}

		// Collect tool_use blocks from this turn.
		toolUses := toolUseBlocks(assistant)
		if len(toolUses) == 0 {
			// No tools requested → the model is done (server-side tool results,
			// if any, were already resolved inline by the API).
			res.Messages = messages
			return res, nil
		}

		// Dispatch tools and append their results.
		resultBlocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			block := l.dispatch(ctx, tu, opts, emit)
			resultBlocks = append(resultBlocks, block)
		}
		toolResultMsg := anthropic.NewBetaUserMessage(resultBlocks...)
		record(opts.Recorder, "user", toolResultMsg)
		messages = append(messages, toolResultMsg)

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

// autocompact summarizes the conversation via the model and replaces history
// with the summary. Returns (messages, false) if the summary call fails.
func (l *Loop) autocompact(ctx context.Context, messages []anthropic.BetaMessageParam, opts Options) ([]anthropic.BetaMessageParam, bool) {
	req := compaction.BuildSummaryRequest(messages, opts.Model, 4096)
	req.Betas = api.DefaultBetas
	assistant, _, err := l.streamTurn(ctx, req, nil)
	if err != nil {
		return messages, false
	}
	summary := finalAssistantText(assistant)
	if summary == "" {
		return messages, false
	}
	return compaction.ReplaceWithSummary(summary), true
}

// streamTurn issues one streaming request and accumulates the full assistant
// message, emitting assistant-text events as deltas arrive.
func (l *Loop) streamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, emit Emitter) (anthropic.BetaMessage, string, error) {
	stream := l.client.Stream(ctx, params)

	var acc anthropic.BetaMessage
	var text string
	for stream.Next() {
		ev := stream.Current()
		if err := acc.Accumulate(ev); err != nil {
			return acc, "", fmt.Errorf("accumulate stream event: %w", err)
		}
		// Surface incremental text to the emitter.
		if d := ev.AsContentBlockDelta(); d.Delta.Text != "" {
			text += d.Delta.Text
			if emit != nil {
				emit(Event{Type: "assistant", Text: d.Delta.Text})
			}
		}
	}
	if err := stream.Err(); err != nil {
		return acc, "", fmt.Errorf("stream: %w", err)
	}
	return acc, finalAssistantText(acc), nil
}

// dispatch runs one tool_use: lookup → permission → validate → execute, and
// returns the tool_result block to append to the conversation.
func (l *Loop) dispatch(ctx context.Context, tu anthropic.BetaToolUseBlock, opts Options, emit Emitter) anthropic.BetaContentBlockParamUnion {
	raw, _ := json.Marshal(tu.Input)
	if emit != nil {
		emit(Event{Type: "tool_use", ToolName: tu.Name, ToolUseID: tu.ID, Input: tu.Input})
	}

	errResult := func(msg string) anthropic.BetaContentBlockParamUnion {
		if emit != nil {
			emit(Event{Type: "tool_result", ToolName: tu.Name, ToolUseID: tu.ID, Content: msg, IsError: true})
		}
		return anthropic.NewBetaToolResultBlock(tu.ID, msg, true)
	}

	tool, ok := l.tools.Lookup(tu.Name)
	if !ok {
		return errResult(fmt.Sprintf("No such tool available: %s", tu.Name))
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

	results, err := tool.Execute(ctx, tools.Context{}, raw)
	if err != nil {
		return errResult(fmt.Sprintf("Tool execution error: %v", err))
	}

	// Phase 1: collapse multiple result blocks into one text result.
	var content string
	isErr := false
	for i, r := range results {
		if i > 0 {
			content += "\n"
		}
		content += r.Content
		isErr = isErr || r.IsError
	}
	if emit != nil {
		emit(Event{Type: "tool_result", ToolName: tu.Name, ToolUseID: tu.ID, Content: content, IsError: isErr})
	}
	return anthropic.NewBetaToolResultBlock(tu.ID, content, isErr)
}

// buildToolParams converts the registry's tools into API tool params.
func (l *Loop) buildToolParams(ctx context.Context) ([]anthropic.BetaToolUnionParam, error) {
	names := l.tools.Names()
	out := make([]anthropic.BetaToolUnionParam, 0, len(names))
	for _, name := range names {
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
		Required    []string       `json:"required"`
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
