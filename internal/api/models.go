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
// when the endpoint doesn't say (OpenAI-compatible servers usually don't).
type ModelInfo struct {
	ID            string
	DisplayName   string
	ContextWindow int
	MaxTokens     int
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
	var opts []option.RequestOption
	// An OAuth credential needs the same beta header the messages path sends.
	// /v1/models happens to answer without it today, but the requirement is
	// endpoint-dependent and undocumented per-endpoint — sending it everywhere
	// is what keeps this from breaking silently later.
	if c.cred.IsOAuth() {
		opts = append(opts, option.WithHeaderAdd("anthropic-beta", string(OAuthBeta)))
	}
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
		})
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// openAIModelList is the response shape of GET /v1/models, which every
// OpenAI-compatible server implements. Only `id` is reliably present — context
// window and display name are not part of the spec, so they stay zero/empty and
// the UI omits them rather than inventing numbers.
type openAIModelList struct {
	Data []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// ListModels enumerates the models the configured endpoint serves.
func (p *OpenAIProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
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
		return nil, fmt.Errorf("%s/models returned %d", p.baseURL, resp.StatusCode)
	}
	var list openAIModelList
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("%s/models: %w", p.baseURL, err)
	}
	out := make([]ModelInfo, 0, len(list.Data))
	for _, m := range list.Data {
		if m.ID != "" {
			out = append(out, ModelInfo{ID: m.ID})
		}
	}
	// Anthropic returns newest-first; OpenAI-compatible servers return whatever
	// order they like, so sort for a stable picker.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
