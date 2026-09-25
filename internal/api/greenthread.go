package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// GreenThread AI Console defaults, used when .klaudia/config.toml leaves them
// unset under provider = "greenthread".
const (
	GreenThreadBaseURL = "https://console.gt-syd.gthread.dev"
	GreenThreadModel   = "moonshotai/Kimi-K3"
)

// GreenThreadProvider talks to the GreenThread AI Console, which serves the
// same models over two protocols: Anthropic Messages (/v1/messages) and OpenAI
// Chat Completions (/v1/chat/completions).
//
// Messages is preferred wherever a model offers it. It is Klaudia's native
// shape, so tool_use blocks stream through untranslated and the model's
// reasoning arrives as thinking blocks that stay in the history and go back to
// the model on the next turn. The Chat Completions shim reads only content and
// tool calls, so there the reasoning is discarded and each turn starts without
// it. Only some models offer Messages (the console lists it per model in
// supported_endpoints), so each turn is routed by its model and the rest use
// the shim.
//
// The console is not Anthropic: none of Anthropic's betas, prompt-cache
// markers or server-side tools are sent, and the SDK is kept from reading
// ANTHROPIC_* credentials from the environment.
type GreenThreadProvider struct {
	baseURL     string // console root, without /v1
	apiKey      string
	http        *http.Client
	messages    *Client
	chat        *OpenAIProvider
	temperature *float64

	mu sync.Mutex
	// messagesModels holds the ids whose supported_endpoints include
	// "messages". Nil until /v1/models has answered once.
	messagesModels map[string]bool
}

// NewGreenThreadProvider builds the provider. baseURL is the console's root;
// a trailing /v1 is accepted and trimmed, since that is how the console
// documents its OpenAI-compatible base.
func NewGreenThreadProvider(baseURL, apiKey string, temperature *float64) *GreenThreadProvider {
	root := strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/v1")
	httpc := newHTTPClient()
	sdk := anthropic.NewClient(
		// Without this the SDK adds ANTHROPIC_API_KEY / ANTHROPIC_AUTH_TOKEN /
		// ANTHROPIC_BASE_URL (and profile credentials) from the environment,
		// which would send a user's Anthropic credential to another host.
		option.WithoutEnvironmentDefaults(),
		option.WithAPIKey(apiKey),
		option.WithBaseURL(root),
		option.WithMaxRetries(maxRetries()),
		option.WithHTTPClient(httpc),
	)
	return &GreenThreadProvider{
		baseURL:     root,
		apiKey:      apiKey,
		http:        &http.Client{},
		messages:    &Client{sdk: sdk, cred: Credential{APIKey: apiKey}, httpc: httpc},
		chat:        NewOpenAIProvider(root+"/v1", apiKey, temperature),
		temperature: temperature,
	}
}

// StreamTurn implements Provider.
func (p *GreenThreadProvider) StreamTurn(ctx context.Context, params anthropic.BetaMessageNewParams, sink StreamSink) (anthropic.BetaMessage, error) {
	if !p.speaksMessages(ctx, string(params.Model)) {
		return p.chat.StreamTurn(ctx, params, sink)
	}
	// Client.StreamTurn would add the default betas and cache_control; go
	// straight to the retrying stream with the request stripped to what the
	// console implements.
	params.Betas = nil
	params.Tools = slices.DeleteFunc(slices.Clone(params.Tools), func(t anthropic.BetaToolUnionParam) bool {
		return t.OfTool == nil // server-side tools: the console 400s on them
	})
	if p.temperature != nil {
		params.Temperature = anthropic.Float(*p.temperature)
	}
	return p.messages.streamRetrying(ctx, params, sink, streamIdleTimeout())
}

// speaksMessages reports whether model is served over /v1/messages, asking
// /v1/models the first time. If that lookup fails the turn goes to Chat
// Completions, which every chat model on the console serves, and the lookup is
// tried again next turn.
func (p *GreenThreadProvider) speaksMessages(ctx context.Context, model string) bool {
	p.mu.Lock()
	known := p.messagesModels
	p.mu.Unlock()
	if known == nil {
		if _, err := p.ListModels(ctx); err != nil {
			return false
		}
		p.mu.Lock()
		known = p.messagesModels
		p.mu.Unlock()
	}
	return known[model]
}

// greenThreadModelList is the console's GET /v1/models: the OpenAI list shape
// plus which endpoints each model serves. It also lists audio and image
// models, which a coding agent cannot drive.
type greenThreadModelList struct {
	Data []struct {
		ID                 string   `json:"id"`
		SupportedEndpoints []string `json:"supported_endpoints"`
		ContextLength      int      `json:"context_length"`
	} `json:"data"`
}

// ListModels lists the console's chat models (those serving chat completions
// or messages), sorted by id, and records which of them speak Messages.
func (p *GreenThreadProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s/v1/models returned %d", p.baseURL, resp.StatusCode)
	}
	var list greenThreadModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("%s/v1/models: %w", p.baseURL, err)
	}
	messages := map[string]bool{}
	var out []ModelInfo
	for _, m := range list.Data {
		if m.ID == "" {
			continue
		}
		if slices.Contains(m.SupportedEndpoints, "messages") {
			messages[m.ID] = true
		} else if !slices.Contains(m.SupportedEndpoints, "chat_completions") {
			continue
		}
		out = append(out, ModelInfo{ID: m.ID, ContextWindow: m.ContextLength})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	p.mu.Lock()
	p.messagesModels = messages
	p.mu.Unlock()
	return out, nil
}
