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

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread/klaudia/internal/api"
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

// Options configures a single Run.
type Options struct {
	Prompt    string
	Model     anthropic.Model
	System    string
	MaxTurns  int   // 0 = unlimited
	MaxTokens int64 // 0 = defaultMaxTokens
}

// Result is the outcome of a Run.
type Result struct {
	Text         string // concatenated final assistant text
	NumTurns     int
	StopReason   string
	InputTokens  int64
	OutputTokens int64
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

	var system []anthropic.BetaTextBlockParam
	if opts.System != "" {
		system = []anthropic.BetaTextBlockParam{{Text: opts.System}}
	}

	messages := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(opts.Prompt)),
	}

	var res Result
	for {
		res.NumTurns++

		params := anthropic.BetaMessageNewParams{
			Model:     opts.Model,
			MaxTokens: maxTokens,
			Messages:  messages,
			System:    system,
			Tools:     toolParams,
		}

		assistant, finalText, err := l.streamTurn(ctx, params, emit)
		if err != nil {
			return res, err
		}
		res.StopReason = string(assistant.StopReason)
		res.InputTokens += assistant.Usage.InputTokens
		res.OutputTokens += assistant.Usage.OutputTokens
		res.Text = finalText

		// Collect tool_use blocks from this turn.
		toolUses := toolUseBlocks(assistant)
		if len(toolUses) == 0 {
			// No tools requested → the model is done.
			return res, nil
		}

		// Append the assistant turn, then dispatch tools and append their results.
		messages = append(messages, assistant.ToParam())
		resultBlocks := make([]anthropic.BetaContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			block := l.dispatch(ctx, tu, emit)
			resultBlocks = append(resultBlocks, block)
		}
		messages = append(messages, anthropic.NewBetaUserMessage(resultBlocks...))

		if opts.MaxTurns > 0 && res.NumTurns >= opts.MaxTurns {
			res.StopReason = "max_turns"
			return res, nil
		}
	}
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
func (l *Loop) dispatch(ctx context.Context, tu anthropic.BetaToolUseBlock, emit Emitter) anthropic.BetaContentBlockParamUnion {
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

	decision, err := tool.CheckPermissions(ctx, raw)
	if err != nil {
		return errResult(fmt.Sprintf("Permission check failed: %v", err))
	}
	if decision.Behavior == tools.PermissionDeny || decision.Behavior == tools.PermissionAsk {
		// Phase 1 headless: anything not auto-allowed is denied (no TTY to ask).
		msg := decision.Message
		if msg == "" {
			msg = fmt.Sprintf("Permission denied for tool %s", tu.Name)
		}
		return errResult(msg)
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
