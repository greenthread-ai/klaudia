// Package api wraps the official anthropic-sdk-go: it resolves credentials
// (env API key or Claude Code OAuth session), applies the Claude Code default
// headers/betas, resolves model aliases, and exposes a streaming Messages call.
//
// We deliberately use the Beta Messages surface because Klaudia relies on beta
// features (claude-code, context-management, server-side web tools).
package api

import (
	"context"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// DefaultBetas is the Claude Code default beta set (Cn1 in 03-providers.js).
// These identify the client as Claude Code and enable context management.
var DefaultBetas = []anthropic.AnthropicBeta{
	"claude-code-20250219",
	"context-management-2025-06-27",
}

// WebToolBetas are the additional betas required when the server-side
// web_search / web_fetch tools are enabled.
var WebToolBetas = []anthropic.AnthropicBeta{
	"web-search-2025-03-05",
	"web-fetch-2025-09-10",
}

// DefaultModel is used when no --model is given. Server-side resolution may
// pick a newer snapshot; this is the alias we send.
const DefaultModel = "claude-sonnet-4-5"

// modelAliases maps the short CLI aliases to model IDs (resolveModelId,
// 07-app-features.js:7901). Full IDs pass through unchanged.
var modelAliases = map[string]string{
	"haiku":  "claude-haiku-4-5",
	"sonnet": "claude-sonnet-4-5",
	"opus":   "claude-opus-4-5",
}

// ResolveModel turns a CLI --model value into a model ID. Empty → DefaultModel.
func ResolveModel(m string) anthropic.Model {
	m = strings.TrimSpace(m)
	if m == "" {
		return anthropic.Model(DefaultModel)
	}
	if full, ok := modelAliases[strings.ToLower(m)]; ok {
		return anthropic.Model(full)
	}
	return anthropic.Model(m)
}

// Client is Klaudia's Anthropic API client: the SDK client plus the resolved
// credential (so callers know whether they are on the OAuth path).
type Client struct {
	sdk  anthropic.Client
	cred Credential
}

// New builds a Client from the resolved credential and optional custom base URL
// (--custom-endpoint). baseURL == "" uses the Anthropic production API.
func New(cred Credential, baseURL string) *Client {
	opts := []option.RequestOption{
		option.WithHeader("x-app", "cli"),
	}
	if cred.IsOAuth() {
		opts = append(opts, option.WithAuthToken(cred.AuthToken))
	} else {
		opts = append(opts, option.WithAPIKey(cred.APIKey))
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Client{sdk: anthropic.NewClient(opts...), cred: cred}
}

// IsOAuth reports whether this client authenticates via OAuth bearer token.
func (c *Client) IsOAuth() bool { return c.cred.IsOAuth() }

// Stream issues a streaming Beta Messages request, injecting the default betas
// unless the caller already set some.
func (c *Client) Stream(ctx context.Context, params anthropic.BetaMessageNewParams) *ssestream.Stream[anthropic.BetaRawMessageStreamEventUnion] {
	if len(params.Betas) == 0 {
		params.Betas = DefaultBetas
	}
	return c.sdk.Beta.Messages.NewStreaming(ctx, params)
}
