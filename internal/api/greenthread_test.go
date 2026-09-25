package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// fakeConsole mimics the GreenThread console: /v1/models in its OpenAI shape
// with supported_endpoints, a streamed /v1/messages answer shaped like the one
// captured from the real console for Kimi K3 (thinking, text, tool_use), and a
// streamed /v1/chat/completions answer. It records each request it is sent.
type fakeConsole struct {
	*httptest.Server
	mu   sync.Mutex
	reqs map[string][]capturedReq
}

type capturedReq struct {
	header http.Header
	body   []byte
}

func newFakeConsole(t *testing.T) *fakeConsole {
	t.Helper()
	fc := &fakeConsole{reqs: map[string][]capturedReq{}}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fc.mu.Lock()
		fc.reqs[r.URL.Path] = append(fc.reqs[r.URL.Path], capturedReq{r.Header.Clone(), body})
		fc.mu.Unlock()
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"object":"list","data":[
				{"id":"moonshotai/Kimi-K3","model_type":"chat","supported_endpoints":["chat_completions","completions","responses","messages"]},
				{"id":"openai/gpt-oss-20b","model_type":"chat","supported_endpoints":["chat_completions","completions","responses"],"context_length":8192},
				{"id":"MiniMaxAI/MiniMax-Music3","model_type":"music","supported_endpoints":["audio_speech"]}]}`)
		case "/v1/messages":
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range []string{
				`{"type":"message_start","message":{"id":"chatcmpl-1","type":"message","role":"assistant","content":[],"model":"moonshotai/Kimi-K3","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":193,"output_tokens":0}}}`,
				`{"type":"content_block_start","content_block":{"type":"thinking","thinking":""},"index":0}`,
				`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"The user wants hello.txt."},"index":0}`,
				`{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"640a9454"},"index":0}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","content_block":{"type":"text","text":""},"index":1}`,
				`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Reading it."},"index":1}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"content_block_start","content_block":{"type":"tool_use","id":"chatcmpl-tool-1","name":"Read","input":{}},"index":2}`,
				`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"file_path\": \"hello.txt\"}"},"index":2}`,
				`{"type":"content_block_stop","index":2}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":193,"output_tokens":75}}`,
				`{"type":"message_stop"}`,
			} {
				var typ struct{ Type string }
				_ = json.Unmarshal([]byte(ev), &typ)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ.Type, ev)
			}
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fc.Close)
	return fc
}

func (fc *fakeConsole) requests(path string) []capturedReq {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.reqs[path]
}

// agentParams is what the agent loop sends every turn: Klaudia's default
// betas plus the server-side web tools, next to an ordinary client tool.
func agentParams(model string) anthropic.BetaMessageNewParams {
	return anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 32000,
		System:    []anthropic.BetaTextBlockParam{{Text: "You are a coding agent."}},
		Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("read hello.txt"))},
		Tools: []anthropic.BetaToolUnionParam{
			{OfTool: &anthropic.BetaToolParam{
				Name:        "Read",
				InputSchema: anthropic.BetaToolInputSchemaParam{Properties: map[string]any{"file_path": map[string]any{"type": "string"}}},
			}},
			{OfWebSearchTool20260318: &anthropic.BetaWebSearchTool20260318Param{AllowedCallers: []string{"direct"}}},
			{OfWebFetchTool20260318: &anthropic.BetaWebFetchTool20260318Param{AllowedCallers: []string{"direct"}}},
		},
		Betas: append(append([]anthropic.AnthropicBeta{}, DefaultBetas...), WebToolBetas...),
	}
}

func TestGreenThreadSendsKimiOverMessagesWithoutAnthropicExtras(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	// A user's Anthropic credentials must never reach the console. Left to its
	// defaults the SDK would pick these up and send them.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "anthropic-oauth-secret")
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.invalid")

	fc := newFakeConsole(t)
	p := NewGreenThreadProvider(fc.URL, "gt_live_test", nil)
	msg, err := p.StreamTurn(context.Background(), agentParams(GreenThreadModel), StreamSink{})
	if err != nil {
		t.Fatal(err)
	}

	reqs := fc.requests("/v1/messages")
	if len(reqs) != 1 {
		t.Fatalf("/v1/messages got %d requests, want 1 (chat completions got %d)", len(reqs), len(fc.requests("/v1/chat/completions")))
	}
	h, body := reqs[0].header, string(reqs[0].body)
	if got := h.Get("X-Api-Key"); got != "gt_live_test" {
		t.Errorf("x-api-key = %q, want the GreenThread key", got)
	}
	for k, vs := range h {
		for _, v := range vs {
			if strings.Contains(v, "anthropic-oauth-secret") {
				t.Errorf("header %s carries the Anthropic credential", k)
			}
		}
	}
	for _, k := range []string{"Anthropic-Beta", "X-App"} {
		if v := h.Get(k); v != "" {
			t.Errorf("%s = %q, want none on the console", k, v)
		}
	}
	for _, s := range []string{"cache_control", "web_search", "web_fetch"} {
		if strings.Contains(body, s) {
			t.Errorf("request body contains %q: %s", s, body)
		}
	}
	if !strings.Contains(body, `"name":"Read"`) {
		t.Errorf("client tool dropped from request: %s", body)
	}

	// The reply is Klaudia's native shape: thinking survives to go back into
	// the history, and the tool call is intact.
	if len(msg.Content) != 3 || msg.Content[0].Type != "thinking" || msg.Content[2].Type != "tool_use" {
		t.Fatalf("content = %+v, want thinking, text, tool_use", msg.Content)
	}
	if msg.Content[0].Signature != "640a9454" {
		t.Errorf("thinking signature = %q", msg.Content[0].Signature)
	}
	if string(msg.Content[2].Input) != `{"file_path": "hello.txt"}` || msg.StopReason != "tool_use" {
		t.Errorf("tool_use input = %s, stop = %s", msg.Content[2].Input, msg.StopReason)
	}
}

func TestGreenThreadSendsOtherModelsOverChatCompletions(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	fc := newFakeConsole(t)
	// The console documents its OpenAI base with /v1; either form works.
	p := NewGreenThreadProvider(fc.URL+"/v1/", "gt_live_test", nil)
	msg, err := p.StreamTurn(context.Background(), agentParams("openai/gpt-oss-20b"), StreamSink{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(fc.requests("/v1/messages")); n != 0 {
		t.Errorf("/v1/messages got %d requests for a model that does not list it", n)
	}
	reqs := fc.requests("/v1/chat/completions")
	if len(reqs) != 1 {
		t.Fatalf("/v1/chat/completions got %d requests, want 1", len(reqs))
	}
	if got := reqs[0].header.Get("Authorization"); got != "Bearer gt_live_test" {
		t.Errorf("Authorization = %q", got)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "hi" {
		t.Errorf("content = %+v", msg.Content)
	}
	// /v1/models is asked once, not every turn.
	if _, err := p.StreamTurn(context.Background(), agentParams(GreenThreadModel), StreamSink{}); err != nil {
		t.Fatal(err)
	}
	if n := len(fc.requests("/v1/models")); n != 1 {
		t.Errorf("/v1/models asked %d times, want 1", n)
	}
}

func TestGreenThreadListModelsKeepsChatModelsOnly(t *testing.T) {
	fc := newFakeConsole(t)
	got, err := NewGreenThreadProvider(fc.URL, "gt_live_test", nil).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != GreenThreadModel || got[1].ID != "openai/gpt-oss-20b" {
		t.Fatalf("got %+v, want Kimi then gpt-oss, and no music model", got)
	}
	if got[0].ContextWindow != 0 || got[1].ContextWindow != 8192 {
		t.Errorf("context windows = %d, %d; want the console's context_length, 0 when absent",
			got[0].ContextWindow, got[1].ContextWindow)
	}
	if h := fc.requests("/v1/models")[0].header.Get("Authorization"); h != "Bearer gt_live_test" {
		t.Errorf("Authorization = %q", h)
	}
}

func TestGreenThreadModelDefaults(t *testing.T) {
	if n, src := ContextWindow(GreenThreadModel, 0); n != 1_000_000 || src != ContextSourceModel {
		t.Errorf("ContextWindow = %d (%s), want 1000000 from the model table", n, src)
	}
	if n, _ := ContextWindow(GreenThreadModel, 262144); n != 262144 {
		t.Errorf("a contextWindow override must win; got %d", n)
	}
	if n := MaxOutputTokens(GreenThreadModel); n != 32000 {
		t.Errorf("MaxOutputTokens = %d, want 32000", n)
	}
}

// TestGreenThreadLive runs one tool-calling turn against the real console.
// Skipped unless GREENTHREAD_API_KEY is set.
func TestGreenThreadLive(t *testing.T) {
	key := os.Getenv("GREENTHREAD_API_KEY")
	if testing.Short() || key == "" {
		t.Skip("set GREENTHREAD_API_KEY to run against the console")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	p := NewGreenThreadProvider(GreenThreadBaseURL, key, nil)
	params := agentParams(GreenThreadModel)
	params.MaxTokens = 4000
	msg, err := p.StreamTurn(ctx, params, StreamSink{})
	if err != nil {
		t.Fatalf("live turn: %v", err)
	}
	var tool *anthropic.BetaContentBlockUnion
	for i := range msg.Content {
		if msg.Content[i].Type == "tool_use" {
			tool = &msg.Content[i]
		}
	}
	if tool == nil || tool.Name != "Read" || !strings.Contains(string(tool.Input), "hello.txt") {
		t.Fatalf("want a Read(hello.txt) tool call; got %+v", msg.Content)
	}
}
