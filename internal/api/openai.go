package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// OpenAIProvider talks to an OpenAI-compatible Chat Completions endpoint
// (e.g. GreenThread's GPT-5.5). It implements Provider by translating the
// canonical Anthropic request into the Chat Completions schema, streaming the
// SSE response, and assembling an anthropic.BetaMessage so the rest of Klaudia
// (loop, tools, sessions, compaction) is unchanged.
type OpenAIProvider struct {
	baseURL     string // e.g. https://api.demo.gthread.dev/v1
	apiKey      string
	temperature *float64
	http        *http.Client
}

// NewOpenAIProvider builds the provider. baseURL should include the /v1 suffix.
// temperature may be nil (omit from request) or a pointer to a value.
func NewOpenAIProvider(baseURL, apiKey string, temperature *float64) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		temperature: temperature,
		http:        &http.Client{},
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
	ID        string          `json:"id,omitempty"`          // tool_use
	Name      string          `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // tool_result
	Content   json.RawMessage `json:"content,omitempty"`     // tool_result (string or blocks)
	Source    json.RawMessage `json:"source,omitempty"`      // image
}

// --- OpenAI Chat Completions shapes ---

type oaMessage struct {
	Role       string       `json:"role"`
	Content    any          `json:"content,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
	ToolCalls  []oaToolCall `json:"tool_calls,omitempty"`
}

type oaContentPart struct {
	Type     string      `json:"type"`
	Text     string      `json:"text,omitempty"`
	ImageURL *oaImageURL `json:"image_url,omitempty"`
}

type oaImageURL struct {
	URL string `json:"url"`
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
	Model               string        `json:"model"`
	Messages            []oaMessage   `json:"messages"`
	Tools               []oaTool      `json:"tools,omitempty"`
	Temperature         *float64      `json:"temperature,omitempty"` // optional per OpenAI spec; omitted when nil
	MaxCompletionTokens int64         `json:"max_completion_tokens,omitempty"`
	Stream              bool          `json:"stream"`
	StreamOptions       *oaStreamOpts `json:"stream_options,omitempty"`
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

	// Same idle-watchdog + stall-retry policy as the Anthropic provider: a
	// half-open SSE connection would otherwise park bufio.Scanner.Scan() forever
	// with the TUI stuck on "thinking…". See provider.go for the rationale.
	idle := streamIdleTimeout()
	for attempt := 0; ; attempt++ {
		acc, delivered, stalled, serr := p.streamAttempt(ctx, body, string(params.Model), sink, idle)
		if !stalled {
			return acc, serr
		}
		if !delivered && attempt < maxStreamStallRetries {
			continue
		}
		return acc, fmt.Errorf("%w: no data for %s: %w", ErrStreamStalled, idle, serr)
	}
}

// streamAttempt issues one Chat Completions request and consumes its SSE
// response under an idle watchdog. It returns the assembled message, whether
// any output reached the sink, whether the watchdog tripped (a stall distinct
// from a user interrupt), and the terminal error. The watchdog cancels only a
// child of ctx, so a tripped watchdog with ctx still live marks a genuine
// stall; a cancelled ctx is the user's interrupt and is returned as-is.
func (p *OpenAIProvider) streamAttempt(ctx context.Context, body []byte, model string, sink StreamSink, idle time.Duration) (acc anthropic.BetaMessage, delivered, stalled bool, err error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return anthropic.BetaMessage{}, false, false, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.doWithRetry(httpReq, body)
	if err != nil {
		return anthropic.BetaMessage{}, false, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		b, _ := readAll(resp.Body, 4096)
		return anthropic.BetaMessage{}, false, false, &OpenAIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(b)}
	}

	var tripped atomic.Bool
	var timer *time.Timer
	if idle > 0 {
		timer = time.AfterFunc(idle, func() {
			tripped.Store(true)
			cancel() // aborts the in-flight resp.Body read → Scan() unblocks
		})
		defer timer.Stop()
	}
	onActivity := func() {
		if timer != nil {
			timer.Reset(idle)
		}
	}

	acc, delivered, err = p.consumeStream(resp.Body, model, sink, onActivity)
	if tripped.Load() && ctx.Err() == nil {
		return acc, delivered, true, context.DeadlineExceeded
	}
	return acc, delivered, false, err
}

// translateRequest converts the Anthropic params into an OpenAI chat request.
func (p *OpenAIProvider) translateRequest(params anthropic.BetaMessageNewParams) (oaRequest, error) {
	out := oaRequest{
		Model:               string(params.Model),
		Temperature:         p.temperature,
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
				Content:    toolResultContent(b.Content),
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

// toolResultContent extracts text and image blocks from an Anthropic tool_result
// content field. Image blocks become OpenAI image_url parts using data URLs.
func toolResultContent(raw json.RawMessage) any {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []aBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}

	parts := make([]oaContentPart, 0, len(blocks))
	var text strings.Builder
	hasImage := false
	flushText := func() {
		if text.Len() == 0 {
			return
		}
		parts = append(parts, oaContentPart{Type: "text", Text: text.String()})
		text.Reset()
	}
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			text.WriteString(blk.Text)
		case "image":
			flushText()
			if p, ok := imagePart(blk); ok {
				parts = append(parts, p)
				hasImage = true
			}
		}
	}
	flushText()
	if !hasImage {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return parts
}

func imagePart(blk aBlock) (oaContentPart, bool) {
	var source struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	}
	if json.Unmarshal(blk.Source, &source) != nil {
		return oaContentPart{}, false
	}
	url := source.URL
	if url == "" && source.Type == "base64" && source.MediaType != "" && source.Data != "" {
		url = "data:" + source.MediaType + ";base64," + source.Data
	}
	if url == "" {
		return oaContentPart{}, false
	}
	return oaContentPart{Type: "image_url", ImageURL: &oaImageURL{URL: url}}, true
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

// OpenAIErrorPayload is the structured error envelope OpenAI-compatible
// providers return inside the response body:
//
//	{"error": {"message": "...", "type": "...", "code": "..."}}
//
// Surfacing it lets FriendlyError pass through the provider's own (often
// actionable) wording instead of a generic guess.
type OpenAIErrorPayload struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	// Code is provider-dependent: most OpenAI-compatible APIs send a string
	// like "insufficient_quota", but some (e.g. gpt-oss-120b hosts) send a
	// numeric HTTP status. json.RawMessage accepts both shapes without
	// erroring the whole envelope parse.
	Code json.RawMessage `json:"code"`
}

// Payload parses the response body for that envelope. Returns nil when the
// body is empty or doesn't match (some providers ship plain-text errors).
func (e *OpenAIError) Payload() *OpenAIErrorPayload {
	if e == nil || e.Body == "" {
		return nil
	}
	var wrap struct {
		Error OpenAIErrorPayload `json:"error"`
	}
	if err := json.Unmarshal([]byte(e.Body), &wrap); err != nil {
		return nil
	}
	if wrap.Error.Message == "" && wrap.Error.Type == "" && len(wrap.Error.Code) == 0 {
		return nil
	}
	return &wrap.Error
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
				_ = resp.Body.Close()
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
