package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// OpenAIProvider talks to an OpenAI-compatible Chat Completions endpoint
// (e.g. GreenThread's GPT-5.5). It implements Provider by translating the
// canonical Anthropic request into the Chat Completions schema, streaming the
// SSE response, and assembling an anthropic.BetaMessage so the rest of Klaudia
// (loop, tools, sessions, compaction) is unchanged.
type OpenAIProvider struct {
	baseURL string // e.g. https://api.demo.gthread.dev/v1
	apiKey  string
	http    *http.Client
}

// NewOpenAIProvider builds the provider. baseURL should include the /v1 suffix.
func NewOpenAIProvider(baseURL, apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

// --- Anthropic wire shapes we read from the request params ---

type aMsg struct {
	Role    string   `json:"role"`
	Content []aBlock `json:"content"`
}

type aBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`         // tool_use
	Name      string          `json:"name,omitempty"`       // tool_use
	Input     json.RawMessage `json:"input,omitempty"`      // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
	Content   json.RawMessage `json:"content,omitempty"`    // tool_result (string or blocks)
}

// --- OpenAI Chat Completions shapes ---

type oaMessage struct {
	Role       string       `json:"role"`
	Content    string       `json:"content,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
}

type oaToolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type oaRequest struct {
	Model               string         `json:"model"`
	Messages            []oaMessage    `json:"messages"`
	Tools               []oaTool       `json:"tools,omitempty"`
	Temperature         float64        `json:"temperature"`
	MaxCompletionTokens int64          `json:"max_completion_tokens,omitempty"`
	Stream              bool           `json:"stream"`
	StreamOptions       *oaStreamOpts  `json:"stream_options,omitempty"`
}

type oaStreamOpts struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters,omitempty"`
	} `json:"function"`
}

// StreamTurn implements Provider.
func (p *OpenAIProvider) StreamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink) (anthropic.BetaMessage, error) {
	reqBody, err := p.translateRequest(params)
	if err != nil {
		return anthropic.BetaMessage{}, err
	}
	body, _ := json.Marshal(reqBody)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return anthropic.BetaMessage{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.doWithRetry(httpReq, body)
	if err != nil {
		return anthropic.BetaMessage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := readAll(resp.Body, 4096)
		return anthropic.BetaMessage{}, &OpenAIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(b)}
	}

	return p.consumeStream(resp.Body, string(params.Model), sink)
}

// translateRequest converts the Anthropic params into an OpenAI chat request.
func (p *OpenAIProvider) translateRequest(params anthropic.BetaMessageNewParams) (oaRequest, error) {
	out := oaRequest{
		Model:               string(params.Model),
		Temperature:         1,
		MaxCompletionTokens: params.MaxTokens,
		Stream:              true,
		StreamOptions:       &oaStreamOpts{IncludeUsage: true},
	}

	// System prompt → a leading system message.
	var sys strings.Builder
	for _, blk := range params.System {
		sys.WriteString(blk.Text)
	}
	if sys.Len() > 0 {
		out.Messages = append(out.Messages, oaMessage{Role: "system", Content: sys.String()})
	}

	// Conversation messages.
	rawMsgs, _ := json.Marshal(params.Messages)
	var amsgs []aMsg
	if err := json.Unmarshal(rawMsgs, &amsgs); err != nil {
		return out, fmt.Errorf("translate messages: %w", err)
	}
	for _, m := range amsgs {
		out.Messages = append(out.Messages, translateMessage(m)...)
	}

	// Custom tools → function tools (skip server-side web tools, which have no
	// input_schema and no OpenAI equivalent).
	rawTools, _ := json.Marshal(params.Tools)
	var atools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	_ = json.Unmarshal(rawTools, &atools)
	for _, t := range atools {
		if t.Name == "" || len(t.InputSchema) == 0 {
			continue
		}
		var ot oaTool
		ot.Type = "function"
		ot.Function.Name = t.Name
		ot.Function.Description = t.Description
		var schema any
		_ = json.Unmarshal(t.InputSchema, &schema)
		ot.Function.Parameters = schema
		out.Tools = append(out.Tools, ot)
	}
	return out, nil
}

// translateMessage converts one Anthropic message into one or more OpenAI
// messages (tool results become separate role:"tool" messages).
func translateMessage(m aMsg) []oaMessage {
	var text strings.Builder
	var toolCalls []oaToolCall
	var toolResults []oaMessage

	for _, b := range m.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			tc := oaToolCall{ID: b.ID, Type: "function"}
			tc.Function.Name = b.Name
			tc.Function.Arguments = string(b.Input)
			if tc.Function.Arguments == "" {
				tc.Function.Arguments = "{}"
			}
			toolCalls = append(toolCalls, tc)
		case "tool_result":
			toolResults = append(toolResults, oaMessage{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    toolResultText(b.Content),
			})
		}
	}

	var msgs []oaMessage
	if m.Role == "assistant" {
		am := oaMessage{Role: "assistant", Content: text.String(), ToolCalls: toolCalls}
		msgs = append(msgs, am)
	} else { // user
		if text.Len() > 0 {
			msgs = append(msgs, oaMessage{Role: "user", Content: text.String()})
		}
		msgs = append(msgs, toolResults...)
	}
	return msgs
}

// toolResultText extracts the text from a tool_result content field, which may
// be a plain string or an array of content blocks.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []aBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			b.WriteString(blk.Text)
		}
		return b.String()
	}
	return ""
}

func readAll(r interface{ Read([]byte) (int, error) }, max int) (string, error) {
	buf := make([]byte, max)
	n, _ := r.Read(buf)
	return string(buf[:n]), nil
}

// OpenAIError is a non-2xx response from the OpenAI-compatible endpoint. It
// carries the status so FriendlyError can classify it like an Anthropic error.
type OpenAIError struct {
	StatusCode int
	Body       string
}

func (e *OpenAIError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("openai endpoint %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("openai endpoint %d", e.StatusCode)
}

// doWithRetry issues the request, retrying transient failures (connection
// errors and 429/5xx) with exponential backoff that honors Retry-After. The
// request body is re-created from bodyBytes for each attempt. It mirrors the
// Anthropic SDK's retry behavior for parity across providers.
func (p *OpenAIProvider) doWithRetry(req *http.Request, bodyBytes []byte) (*http.Response, error) {
	max := maxRetries()
	var lastErr error
	for attempt := 0; attempt <= max; attempt++ {
		if attempt > 0 {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff(attempt)):
			}
		}
		clone := req.Clone(req.Context())
		clone.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		clone.ContentLength = int64(len(bodyBytes))

		resp, err := p.http.Do(clone)
		if err != nil {
			lastErr = err
			continue // connection error → retry
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			if attempt < max {
				resp.Body.Close()
				lastErr = &OpenAIError{StatusCode: resp.StatusCode}
				continue
			}
		}
		return resp, nil
	}
	return nil, lastErr
}

// backoff returns an exponential delay for the given (1-based) attempt.
func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	return d
}
