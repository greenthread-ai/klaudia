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

// claudeCodeVersion appears inside the billing-header system block. The value
// is logged by Anthropic for analytics but not validated — bump it when a new
// claude release lands if you want the analytics to stay accurate; nothing
// breaks if it lags.
const claudeCodeVersion = "2.1.153"

// WebToolBetas are the additional betas required when the server-side
// web_search / web_fetch tools are enabled.
var WebToolBetas = []anthropic.AnthropicBeta{
	"web-search-2025-03-05",
	"web-fetch-2025-09-10",
}

// DefaultModel is used when no --model is given. Server-side resolution may
// pick a newer snapshot; this is the alias we send.
const DefaultModel = "claude-opus-5"

// modelAliases maps the short CLI aliases to current model IDs (resolveModelId,
// 07-app-features.js:7901). Full IDs pass through unchanged, so users can
// always pin a specific snapshot via `--model claude-opus-4-8` or
// `--model claude-opus-4-5-20251101`. Bumped to track the current Claude 4.x
// lineup — verified live against /v1/messages with a one-shot probe.
var modelAliases = map[string]string{
	"haiku":  "claude-haiku-4-5",
	"sonnet": "claude-sonnet-5",
	"opus":   "claude-opus-5",
	"fable":  "claude-fable-5",
}

// modelContextWindows is the offline fallback for the input-token limit, used
// before (or instead of) a live answer from the provider's models endpoint —
// which is authoritative and is what /model now records when you pick a model.
//
// These values were verified against GET /v1/models rather than assumed. The
// previous table claimed 200K across the board on the theory that 1M needed
// `context-1m-2025-08-07`, which DefaultBetas doesn't send; the API reports 1M
// for the current lineup regardless, so the status bar's `ctx N%` had been
// overstating context pressure roughly fivefold.
var modelContextWindows = map[string]int{
	"claude-opus-5":              1_000_000,
	"claude-sonnet-5":            1_000_000,
	"claude-fable-5":             1_000_000,
	"claude-opus-4-8":            1_000_000,
	"claude-opus-4-7":            1_000_000,
	"claude-opus-4-6":            1_000_000,
	"claude-sonnet-4-6":          1_000_000,
	"claude-sonnet-4-5-20250929": 1_000_000,
	"claude-opus-4-5-20251101":   200_000,
	"claude-haiku-4-5":           200_000,
	"claude-haiku-4-5-20251001":  200_000,
}

// ContextWindow source labels for /stats and /doctor reporting.
const (
	ContextSourceConfig  = "config override"
	ContextSourceModel   = "model default"
	ContextSourceUnknown = "unknown — using compaction fallback"
)

// ContextWindow returns the effective input-token limit and a human-readable
// source label. Precedence: an explicit positive override (cfg.ContextWindow,
// the OpenAI-compat escape hatch) wins; otherwise the per-model table; finally
// a generic "unknown" with the compaction fallback. Aliases ("opus", "sonnet")
// resolve through ResolveModel so users see the same number regardless of how
// they typed the model name.
func ContextWindow(model string, override int) (limit int, source string) {
	if override > 0 {
		return override, ContextSourceConfig
	}
	resolved := string(ResolveModel(model))
	if n, ok := modelContextWindows[resolved]; ok {
		return n, ContextSourceModel
	}
	return 0, ContextSourceUnknown
}

// DefaultMaxOutputTokens is the per-response output cap for an UNKNOWN model
// (e.g. an OpenAI-compatible endpoint whose limit we can't look up). Kept
// conservative so it is safe anywhere; set cfg.MaxTokens to raise it.
const DefaultMaxOutputTokens = 8192

// modelMaxOutputTokens is each model's real maximum output tokens on the
// synchronous Messages API (docs.claude.com model pages, verified 2026-08).
// These are the true caps, not under-approximations: on Claude 4.5+ a request
// whose input+max_tokens exceeds the context window is accepted and simply
// stops with stop_reason model_context_window_exceeded, so requesting the full
// cap can't 400. Every 1M-context model generates up to 128k; the 200k-context
// models up to 64k. Keep this in sync with modelContextWindows.
var modelMaxOutputTokens = map[string]int{
	// 1M-context models → 128k output.
	"claude-opus-5":     128000,
	"claude-sonnet-5":   128000,
	"claude-fable-5":    128000,
	"claude-opus-4-8":   128000,
	"claude-opus-4-7":   128000,
	"claude-opus-4-6":   128000,
	"claude-sonnet-4-6": 128000,
	// 200k-context models → 64k output.
	"claude-opus-4-5-20251101":   64000,
	"claude-sonnet-4-5-20250929": 64000,
	"claude-haiku-4-5":           64000,
	"claude-haiku-4-5-20251001":  64000,
}

// MaxOutputTokens returns the default output-token cap for a model, resolving
// aliases the same way ContextWindow does. Callers that have an explicit
// override (config, or a live value from the models endpoint) should prefer it;
// this is the offline default the agent loop falls back to.
func MaxOutputTokens(model string) int {
	if n, ok := modelMaxOutputTokens[string(ResolveModel(model))]; ok {
		return n
	}
	return DefaultMaxOutputTokens
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

// claudeCodeBillingPrefix is the literal prefix Anthropic looks for in the
// *first* system block on OAuth requests. Without it, every request — even
// with a perfect set of impersonation headers — falls into a strict default
// bucket and returns instant 429s. The values that follow are observed for
// server-side analytics but NOT validated (verified by sending bogus values
// and watching them all 200), so we put honest klaudia metadata in there.
const claudeCodeBillingPrefix = "x-anthropic-billing-header:"

// augmentSystem prepends Claude Code's billing-header system block on the
// OAuth path. The block must come *first* in the system array — claude itself
// emits it as system[0] and a real `claude -p` against a local proxy confirms
// the format. Idempotent: if the caller already sent a billing block (e.g. on
// retry), don't double up.
func (c *Client) augmentSystem(in []anthropic.BetaTextBlockParam) []anthropic.BetaTextBlockParam {
	if !c.cred.IsOAuth() {
		return in
	}
	if len(in) > 0 && strings.HasPrefix(in[0].Text, claudeCodeBillingPrefix) {
		return in
	}
	billing := anthropic.BetaTextBlockParam{
		Text: claudeCodeBillingPrefix + " cc_version=" + claudeCodeVersion + "; cc_entrypoint=klaudia;",
	}
	return append([]anthropic.BetaTextBlockParam{billing}, in...)
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
//
// On the OAuth path Anthropic's rate-limit bucket is selected entirely by the
// presence of an `x-anthropic-billing-header:` prefix on system[0] — see
// augmentSystem. We previously mirrored claude's UA + Stainless fingerprint to
// fix 429s; turned out none of it was the discriminator. The Go SDK's native
// identity is fine on both paths.
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
