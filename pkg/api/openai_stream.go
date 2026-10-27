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

// consumeStream reads the SSE response, forwards text deltas and a synthesized
// Anthropic-shaped raw-event sequence to sink, and assembles the result into an
// anthropic.BetaMessage. The synthesized events (message_start →
// content_block_delta… → message_stop) are best-effort — there is no native
// Anthropic SSE to forward — but they let partial-message consumers (SDKs,
// editors) receive incremental chunks regardless of provider.
//
// onActivity (nil-safe) fires once per received line so the caller's idle
// watchdog can push its deadline out. The returned bool reports whether any
// observable output (text or, with a raw sink, synthesized events) reached the
// sink — the caller uses it to decide whether a stalled attempt is safe to
// retry transparently.
func (p *OpenAIProvider) consumeStream(body io.Reader, model string, sink StreamSink, onActivity func()) (anthropic.BetaMessage, bool, error) {
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var text strings.Builder
	tools := map[int]*toolAccum{}
	finish := ""
	var inTok, outTok int64
	delivered := false

	// Synthesize the opening events for a single text content block. We emit a
	// message_start with a placeholder message, then a text content_block_start;
	// text deltas follow as content_block_delta. Tool-call blocks are not
	// streamed incrementally (assembled at the end), matching the shim's
	// existing non-streaming assembly of tool_use.
	msgID := "msg_" + uuid.NewString()
	started := false
	startBlocks := func() {
		if started {
			return
		}
		started = true
		if sink.OnRawEvent != nil {
			delivered = true
		}
		sink.raw(synthEvent(map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": msgID, "type": "message", "role": "assistant",
				"model": model, "content": []any{}, "stop_reason": nil,
				"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
			},
		}))
		sink.raw(synthEvent(map[string]any{
			"type": "content_block_start", "index": 0,
			"content_block": map[string]any{"type": "text", "text": ""},
		}))
	}

	for sc.Scan() {
		if onActivity != nil {
			onActivity() // a line arrived — reset the idle watchdog
		}
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
				startBlocks()
				delivered = true
				text.WriteString(ch.Delta.Content)
				sink.text(ch.Delta.Content)
				sink.raw(synthEvent(map[string]any{
					"type": "content_block_delta", "index": 0,
					"delta": map[string]any{"type": "text_delta", "text": ch.Delta.Content},
				}))
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
		return anthropic.BetaMessage{}, delivered, err
	}

	// Close out the synthesized event sequence (only if we opened it — a
	// tool-only turn with no text never emits a text block).
	if started {
		sink.raw(synthEvent(map[string]any{"type": "content_block_stop", "index": 0}))
		sink.raw(synthEvent(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": mapFinishReason(finish, len(tools) > 0)},
			"usage": map[string]any{"output_tokens": outTok},
		}))
		sink.raw(synthEvent(map[string]any{"type": "message_stop"}))
	}

	msg, err := assembleMessage(model, text.String(), tools, finish, inTok, outTok)
	return msg, delivered, err
}

// synthEvent builds a BetaRawMessageStreamEventUnion from a payload map by
// marshaling then unmarshaling it, which populates the union's JSON.raw so
// downstream RawJSON() (used by the partial-message emitter) works.
func synthEvent(payload map[string]any) anthropic.BetaRawMessageStreamEventUnion {
	var ev anthropic.BetaRawMessageStreamEventUnion
	b, _ := json.Marshal(payload)
	_ = ev.UnmarshalJSON(b)
	return ev
}

// assembleMessage builds an anthropic.BetaMessage from the accumulated stream
// by constructing the Messages-API JSON and unmarshaling it (robustly handles
// the content-block union types).
func assembleMessage(model, text string, tools map[int]*toolAccum, finish string, inTok, outTok int64) (anthropic.BetaMessage, error) {
	var content []map[string]any
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	// A refusal / empty response (the model returned no text AND no tool calls)
	// otherwise marshals to "content": null, which the Anthropic API later
	// rejects on resume ("messages.<i>.content: Field required"). Synthesize a
	// visible placeholder block so the refusal is recorded honestly, the
	// transcript stays well-formed, and the model can see what happened.
	if text == "" && len(tools) == 0 {
		reason := finish
		if reason == "" {
			reason = "empty response"
		}
		content = append(content, map[string]any{
			"type": "text",
			"text": "[Provider returned no content — finish_reason=" + reason + "]",
		})
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
