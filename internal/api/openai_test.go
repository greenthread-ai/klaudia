package api

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestTranslateRequest(t *testing.T) {
	p := NewOpenAIProvider("https://x/v1", "k")
	params := anthropic.BetaMessageNewParams{
		Model:     "openai/gpt-5.5",
		MaxTokens: 1024,
		System:    []anthropic.BetaTextBlockParam{{Text: "be terse"}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hello")),
		},
		Tools: []anthropic.BetaToolUnionParam{
			{OfTool: &anthropic.BetaToolParam{
				Name:        "Read",
				Description: anthropic.String("read a file"),
				InputSchema: anthropic.BetaToolInputSchemaParam{
					Properties: map[string]any{"file_path": map[string]any{"type": "string"}},
					Required:   []string{"file_path"},
				},
			}},
			// A server-side web tool must be dropped (no input_schema).
			{OfWebSearchTool20250305: &anthropic.BetaWebSearchTool20250305Param{}},
		},
	}

	req, err := p.translateRequest(params)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "openai/gpt-5.5" || !req.Stream || req.MaxCompletionTokens != 1024 {
		t.Errorf("bad scalar fields: %+v", req)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
		t.Fatalf("messages = %+v", req.Messages)
	}
	if req.Messages[1].Content != "hello" {
		t.Errorf("user content = %q", req.Messages[1].Content)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "Read" {
		t.Errorf("expected only the custom Read tool, got %+v", req.Tools)
	}
}

func TestTranslateToolResultMessage(t *testing.T) {
	// An assistant tool_use followed by a user tool_result must become an
	// assistant message with tool_calls and a role:"tool" message.
	id := "toolu_1"
	assistant := anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaToolUseBlock(id, map[string]any{"file_path": "/x"}, "Read")},
	}
	toolRes := anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock(id, "file contents", false))

	p := NewOpenAIProvider("https://x/v1", "k")
	req, err := p.translateRequest(anthropic.BetaMessageNewParams{
		Model:    "m",
		Messages: []anthropic.BetaMessageParam{assistant, toolRes},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != "assistant" || len(req.Messages[0].ToolCalls) != 1 ||
		req.Messages[0].ToolCalls[0].Function.Name != "Read" {
		t.Errorf("assistant tool_calls wrong: %+v", req.Messages[0])
	}
	if req.Messages[1].Role != "tool" || req.Messages[1].ToolCallID != id ||
		!strings.Contains(req.Messages[1].Content, "file contents") {
		t.Errorf("tool result message wrong: %+v", req.Messages[1])
	}
}

func TestConsumeStreamAssembles(t *testing.T) {
	// A canned OpenAI SSE stream: two text deltas, a tool call, then DONE.
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"Hello "}}]}`,
		`data: {"choices":[{"delta":{"content":"world"}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"Read","arguments":"{\"file_path\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"/tmp/x\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":8}}`,
		`data: [DONE]`,
	}, "\n")

	p := NewOpenAIProvider("https://x/v1", "k")
	var streamed strings.Builder
	msg, err := p.consumeStream(strings.NewReader(sse), "openai/gpt-5.5", func(s string) { streamed.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if streamed.String() != "Hello world" {
		t.Errorf("streamed text = %q", streamed.String())
	}
	if string(msg.StopReason) != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", msg.StopReason)
	}
	if msg.Usage.InputTokens != 12 || msg.Usage.OutputTokens != 8 {
		t.Errorf("usage = %+v", msg.Usage)
	}

	var text, toolName, filePath string
	for _, b := range msg.Content {
		switch b.Type {
		case "text":
			text = b.AsText().Text
		case "tool_use":
			tu := b.AsToolUse()
			toolName = tu.Name
			if in, ok := tu.Input.(map[string]any); ok {
				filePath, _ = in["file_path"].(string)
			}
		}
	}
	if text != "Hello world" || toolName != "Read" || filePath != "/tmp/x" {
		t.Errorf("assembled blocks wrong: text=%q tool=%q path=%q", text, toolName, filePath)
	}
}
