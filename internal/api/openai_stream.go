package api

import (
	"bufio"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
)

// oaChunk is one streamed Chat Completions SSE event.
type oaChunk struct {
	Choices []struct {
		Delta struct {
			Content   string       `json:"content"`
			ToolCalls []oaToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

// toolAccum accumulates a streamed tool call across delta fragments.
type toolAccum struct {
	id   string
	name string
	args strings.Builder
}

// consumeStream reads the SSE response, emits text deltas via onText, and
// assembles the result into an anthropic.BetaMessage.
func (p *OpenAIProvider) consumeStream(body io.Reader, model string, onText func(string)) (anthropic.BetaMessage, error) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var text strings.Builder
	tools := map[int]*toolAccum{}
	finish := ""
	var inTok, outTok int64

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		var chunk oaChunk
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Usage != nil {
			inTok, outTok = chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				if onText != nil {
					onText(ch.Delta.Content)
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				acc := tools[tc.Index]
				if acc == nil {
					acc = &toolAccum{}
					tools[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args.WriteString(tc.Function.Arguments)
			}
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
		}
	}
	if err := sc.Err(); err != nil {
		return anthropic.BetaMessage{}, err
	}

	return assembleMessage(model, text.String(), tools, finish, inTok, outTok)
}

// assembleMessage builds an anthropic.BetaMessage from the accumulated stream
// by constructing the Messages-API JSON and unmarshaling it (robustly handles
// the content-block union types).
func assembleMessage(model, text string, tools map[int]*toolAccum, finish string, inTok, outTok int64) (anthropic.BetaMessage, error) {
	var content []map[string]any
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	// Emit tool_use blocks in ascending index order.
	idxs := make([]int, 0, len(tools))
	for i := range tools {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		t := tools[i]
		var input any = map[string]any{}
		if s := strings.TrimSpace(t.args.String()); s != "" {
			_ = json.Unmarshal([]byte(s), &input) // tolerate malformed; leave {}
		}
		id := t.id
		if id == "" {
			id = "toolu_" + uuid.NewString()
		}
		content = append(content, map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  t.name,
			"input": input,
		})
	}

	msg := map[string]any{
		"id":          "msg_" + uuid.NewString(),
		"type":        "message",
		"role":        "assistant",
		"model":       model,
		"content":     content,
		"stop_reason": mapFinishReason(finish, len(tools) > 0),
		"usage": map[string]any{
			"input_tokens":  inTok,
			"output_tokens": outTok,
		},
	}

	raw, _ := json.Marshal(msg)
	var out anthropic.BetaMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

// mapFinishReason maps an OpenAI finish_reason to an Anthropic stop_reason.
func mapFinishReason(finish string, hasTools bool) string {
	switch finish {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	case "stop":
		return "end_turn"
	default:
		if hasTools {
			return "tool_use"
		}
		return "end_turn"
	}
}
