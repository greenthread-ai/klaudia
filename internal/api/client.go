// Package api wraps the official anthropic-sdk-go: it resolves credentials
// (env API key or Claude Code OAuth session), applies the Claude Code default
// headers/betas, resolves model aliases, and exposes a streaming Messages call.
//
// We deliberately use the Beta Messages surface because Klaudia relies on beta
// features (claude-code, context-management, server-side web tools).
package api

import (
	"os"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultBetas is the Claude Code default beta set (Cn1 in 03-providers.js).
// These identify the client as Claude Code and enable context management.
var DefaultBetas = []anthropic.AnthropicBeta{
	"claude-code-20250219",
	"context-management-2025-06-27",
}

// OAuthBeta is the header Claude Code attaches when the credential is an OAuth
// token. Without it, the Anthropic API treats the same token as "external" use
// and applies much stricter rate limits — observed in practice as immediate
// 429s on the first turn while `claude` itself, holding the same Keychain
// token, works normally. (03-providers.js: `if (Y7()) q.push(BZ)`.)
const OAuthBeta anthropic.AnthropicBeta = "oauth-2025-04-20"

// WebToolBetas are the additional betas required when the server-side
// web_search / web_fetch tools are enabled.
var WebToolBetas = []anthropic.AnthropicBeta{
	"web-search-2025-03-05",
	"web-fetch-2025-09-10",
}

// DefaultModel is used when no --model is given. Server-side resolution may
// pick a newer snapshot; this is the alias we send.
const DefaultModel = "claude-sonnet-4-6"

// modelAliases maps the short CLI aliases to current model IDs (resolveModelId,
// 07-app-features.js:7901). Full IDs pass through unchanged. Bumped to track
// the current Claude 4.x lineup — Claude Code on Claude Max defaults to Opus
// 4.7, so `--model opus` now matches that experience.
var modelAliases = map[string]string{
	"haiku":  "claude-haiku-4-5",
	"sonnet": "claude-sonnet-4-6",
	"opus":   "claude-opus-4-7",
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

// augmentBetas adds credential-specific betas to a request's beta list — today
// just oauth-2025-04-20 when the credential is an OAuth token. The agent loop
// constructs params.Betas from package-level DefaultBetas + WebToolBetas, so
// adding it here keeps callers ignorant of the credential. Idempotent: a beta
// already in the list is not added again.
func (c *Client) augmentBetas(in []anthropic.AnthropicBeta) []anthropic.AnthropicBeta {
	if !c.cred.IsOAuth() {
		return in
	}
	for _, b := range in {
		if b == OAuthBeta {
			return in
		}
	}
	return append(in, OAuthBeta)
}

// defaultMaxRetries is higher than the SDK default (2) so transient 429s — common
// when an OAuth token is shared with another active session — recover
// transparently. The SDK uses exponential backoff and honors Retry-After.
const defaultMaxRetries = 5

// maxRetries resolves the retry count, overridable via KLAUDIA_MAX_RETRIES.
func maxRetries() int {
	if v := os.Getenv("KLAUDIA_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return defaultMaxRetries
}

// New builds a Client from the resolved credential and optional custom base URL
// (--custom-endpoint). baseURL == "" uses the Anthropic production API.
func New(cred Credential, baseURL string) *Client {
	opts := []option.RequestOption{
		option.WithHeader("x-app", "cli"),
		option.WithMaxRetries(maxRetries()),
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
