package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Model discovery. Both backends Klaudia speaks to can enumerate what they
// serve — Anthropic at GET /v1/models, OpenAI-compatible endpoints at the same
// path by convention — so the model list doesn't have to be a hardcoded table
// that goes stale the day a model ships.

// ModelInfo is one model an endpoint reports. ContextWindow and MaxTokens are 0
// when the endpoint doesn't say, and the rest is empty: the OpenAI spec
// promises only an id, and what else a server says varies by server.
type ModelInfo struct {
	ID            string
	DisplayName   string
	ContextWindow int
	MaxTokens     int
	// Kind is the job the model does (modelkind.go).
	Kind        ModelKind
	Description string
	// Endpoints are the API routes the server will send this model to, in its
	// own spelling ("chat_completions", "audio_speech"); Params the request
	// fields it accepts. Both are as listed, empty when the server is silent.
	Endpoints []string
	Params    []string
	// Inputs and Outputs are its modalities ("text", "image", "audio").
	Inputs  []string
	Outputs []string
}

// Name is the label to show a user: the display name when the endpoint gives
// one, otherwise the raw id.
func (m ModelInfo) Name() string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return m.ID
}

// ModelLister is implemented by providers that can enumerate their models. It
// is deliberately separate from Provider: a backend that can't list models is
// still a perfectly good Provider, so callers type-assert rather than forcing
// every implementation to carry a stub.
type ModelLister interface {
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// ListModels enumerates the models this credential can reach, newest first.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	opts := append([]option.RequestOption(nil), c.listOpts...)
	var out []ModelInfo
	// The SDK auto-pages; the account's list is short enough that fetching it
	// all is cheaper than threading a cursor through the UI.
	iter := c.sdk.Models.ListAutoPaging(ctx, anthropic.ModelListParams{}, opts...)
	for iter.Next() {
		m := iter.Current()
		out = append(out, ModelInfo{
			ID:            m.ID,
			DisplayName:   m.DisplayName,
			ContextWindow: int(m.MaxInputTokens),
			MaxTokens:     int(m.MaxTokens),
			Kind:          ModelKindChat,
		})
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// openAIModelList is the response shape of GET /v1/models, which every
// OpenAI-compatible server implements. Only `id` is in the spec; the rest are
// the extensions servers that host more than chat models add, in the spelling
// OpenRouter popularised (context_length, architecture) and that vLLM-based
// gateways copy. Absent fields stay zero and the UI omits them rather than
// inventing numbers.
//
// top_provider.context_length is deliberately not read. One gateway reported
// 8192 there for models whose context_length said 65536–131072, and enforced
// neither 8192 on input (a ~12k-token prompt was accepted) nor on max_tokens
// (20000 accepted) — measured, not inferred. A figure that describes nothing
// the server does would only shrink the window Klaudia compacts against.
type openAIModelList struct {
	Data []struct {
		ID            string   `json:"id"`
		Description   string   `json:"description"`
		ContextLength int      `json:"context_length"`
		ModelType     string   `json:"model_type"`
		Type          string   `json:"type"`
		Endpoints     []string `json:"supported_endpoints"`
		Params        []string `json:"supported_parameters"`
		Architecture  struct {
			Inputs  []string `json:"input_modalities"`
			Outputs []string `json:"output_modalities"`
		} `json:"architecture"`
	} `json:"data"`
}

// ListModels enumerates the models the configured endpoint serves.
func (p *OpenAIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	p.setAuth(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s/models returned %d", p.baseURL, resp.StatusCode)
	}
	var list openAIModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("%s/models: %w", p.baseURL, err)
	}
	out := make([]ModelInfo, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID == "" {
			continue
		}
		declared := m.ModelType
		if declared == "" {
			declared = m.Type
		}
		out = append(out, ModelInfo{
			ID:            m.ID,
			ContextWindow: m.ContextLength,
			Kind:          classifyModel(m.ID, m.Endpoints, declared, m.Architecture.Inputs, m.Architecture.Outputs),
			Description:   m.Description,
			Endpoints:     m.Endpoints,
			Params:        m.Params,
			Inputs:        m.Architecture.Inputs,
			Outputs:       m.Architecture.Outputs,
		})
	}
	// Anthropic returns newest-first; OpenAI-compatible servers return whatever
	// order they like, so sort for a stable picker.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
