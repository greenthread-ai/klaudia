// Package api wraps the official anthropic-sdk-go: it resolves credentials
// (env API key or Claude Code OAuth session), applies the Claude Code default
// headers/betas, resolves model aliases, and exposes a streaming Messages call.
//
// We deliberately use the Beta Messages surface because Klaudia relies on beta
// features (claude-code, context-management, server-side web tools).
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultBetas are sent on every request that names none: context management,
// which the loop's tool-result clearing relies on. A host that wants more adds
// them with WithPrepare.
var DefaultBetas = []anthropic.AnthropicBeta{
	"context-management-2025-06-27",
}

// WebToolBetas are sent with the request when the server-side web_search /
// web_fetch tools are enabled. The tools are GA now (the current 20260318 types
// need no beta per the docs), but these original feature-enablement betas are
// still accepted — obsolete betas are ignored, not rejected — so they are kept
// as a harmless belt: dropping them is an untestable change to a working path.
var WebToolBetas = []anthropic.AnthropicBeta{
	"web-search-2025-03-05",
	"web-fetch-2025-09-10",
}

// DefaultModel is used when no --model is given. Server-side resolution may
// pick a newer snapshot; this is the alias we send.
const DefaultModel = "claude-opus-5-5"

// modelAliases maps the short CLI aliases to current model IDs (resolveModelId,
// 07-app-features.js:7901). Full IDs pass through unchanged, so users can
// always pin a specific snapshot via `--model claude-opus-4-8` or
// `--model claude-opus-4-5-20251101`. Bumped to track the current Claude 5.5
// lineup — verified live against GET /v1/models, not assumed.
var modelAliases = map[string]string{
	"haiku":  "claude-haiku-5-5",
	"sonnet": "claude-sonnet-5-5",
	"opus":   "claude-opus-5-5",
	"fable":  "claude-fable-5-1",
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
//
// That sweep then overcorrected on one model: Sonnet 4.5 was left at 1M when
// the endpoint reports 200K for it, understating pressure fivefold on the one
// model it got wrong. TestModelTablesAgreeWithEachOther catches that shape now
// (a 1M entry whose output cap is the 200k-model 64k) without needing the
// network.
var modelContextWindows = map[string]int{
	"claude-opus-5-5":            1_000_000,
	"claude-sonnet-5-5":          1_000_000,
	"claude-haiku-5-5":           1_000_000,
	"claude-fable-5-1":           1_000_000,
	"claude-opus-5":              1_000_000,
	"claude-sonnet-5":            1_000_000,
	"claude-fable-5":             1_000_000,
	"claude-opus-4-8":            1_000_000,
	"claude-opus-4-7":            1_000_000,
	"claude-opus-4-6":            1_000_000,
	"claude-sonnet-4-6":          1_000_000,
	"claude-opus-4-5-20251101":   200_000,
	"claude-sonnet-4-5-20250929": 200_000,
	"claude-haiku-4-5":           200_000,
	"claude-haiku-4-5-20251001":  200_000,
}

// ContextWindow source labels for /stats and /doctor reporting. The first two
// are the LimitSource* labels, not copies of their text: the context window and
// the output cap are resolved together by ResolveLimits, and two constant sets
// spelling the same label would drift the moment one was reworded.
const (
	ContextSourceConfig  = LimitSourceConfig
	ContextSourceModel   = LimitSourceModel
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
//
// It is a floor, not a good answer: it is 15x below what the current Claude
// models allow, and a model absent from modelMaxOutputTokens silently writes
// less in a turn than it is able to. Prefer the live figure from ListModels
// (ModelInfo.MaxTokens), which is what the agent loop is given at startup.
//
// doctor.floorOutputTokens repeats this number so /doctor can warn when a run
// has landed on it; the doctor package is a leaf and does not import api. Keep
// the two in step.
const DefaultMaxOutputTokens = 8192

// modelMaxOutputTokens is each model's real maximum output tokens on the
// synchronous Messages API, verified live against GET /v1/models (2026-10)
// rather than read off a docs page. Every 1M-context model generates up to
// 128k; the 200k-context models up to 64k. Keep this in sync with
// modelContextWindows — TestLiveModelTablesMatchProvider checks it for real.
//
// These are the true caps and must not be exceeded. An over-cap max_tokens is
// a 400, measured:
//
//	claude-sonnet-4-5-20250929, max_tokens=128000 →
//	  "max_tokens: 128000 > 64000, which is the maximum allowed number of
//	   output tokens for claude-sonnet-4-5-20250929"
//
// An earlier comment here claimed such a request "can't 400" because Claude
// 4.5+ answers an oversized request with stop_reason
// model_context_window_exceeded. That is true of input+max_tokens against the
// *context window* and false of max_tokens against the *output* cap, which is
// checked up front. The 400 does name the real limit, though, which is what
// MaxTokensTooLarge reads so a stale entry here costs one retry.
var modelMaxOutputTokens = map[string]int{
	// 1M-context models → 128k output.
	"claude-opus-5-5":   128000,
	"claude-sonnet-5-5": 128000,
	"claude-haiku-5-5":  128000,
	"claude-fable-5-1":  128000,
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

// ClaudeProvider reports whether a configured provider name ("" = the default)
// serves Anthropic's model ids, so that Claude's aliases mean something there.
func ClaudeProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "anthropic":
		return true
	}
	return false
}

// ResolveModelFor is ResolveModel for a configured provider. The aliases
// ("sonnet") are Anthropic's names and expand only there: any other provider
// gets the string it was given, trimmed, because "sonnet" sent as
// claude-sonnet-5 to an OpenAI-compatible endpoint, and sized from the Claude
// tables, is a guess the endpoint never asked for. No model at all still means
// DefaultModel on every provider, for a gateway that serves Claude's ids under
// their own names. (From PR #80, which made that case an error instead.)
func ResolveModelFor(provider, m string) anthropic.Model {
	if ClaudeProvider(provider) || strings.TrimSpace(m) == "" {
		return ResolveModel(m)
	}
	return anthropic.Model(strings.TrimSpace(m))
}

// AliasWarning explains, when model is a bare Claude alias used with a
// provider that does not serve Anthropic's ids, that it is sent as written.
// It returns "" otherwise. Callers show it once, when the model is chosen,
// not on every turn that re-resolves it.
func AliasWarning(provider, model string) string {
	if ClaudeProvider(provider) {
		return ""
	}
	full, ok := modelAliases[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		return ""
	}
	return fmt.Sprintf("model %q is a Claude alias, which is expanded only for the Anthropic provider; "+
		"provider %q receives it unchanged. Use the model id your endpoint serves (for Anthropic it would have been %s)",
		strings.TrimSpace(model), strings.TrimSpace(provider), full)
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

// Client is an Anthropic Messages API client: the SDK client plus the
// credential it was built with.
type Client struct {
	sdk    anthropic.Client
	cred   Credential
	tuning Tuning
	// prepare and listOpts are the host's (WithPrepare, WithListOptions).
	prepare  func(*anthropic.BetaMessageNewParams)
	listOpts []option.RequestOption
	// httpc is the client the SDK was built with, kept so a stalled stream can
	// drop its idle connections before retrying — see streamRetrying.
	httpc *http.Client
	// baseURL is the custom endpoint the client was built with ("" for the
	// Anthropic API), kept so a model-not-found error can say where it came
	// from.
	baseURL string
}

// defaultAnthropicEndpoint is what an unset baseURL talks to, named in errors.
const defaultAnthropicEndpoint = "https://api.anthropic.com"

// endpoint is the base URL requests go to, for error messages.
func (c *Client) endpoint() string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return defaultAnthropicEndpoint
}

// dropIdleConnections discards pooled connections, so the next attempt dials
// instead of re-using one that a sleep or NAT timeout has silently killed.
func (c *Client) dropIdleConnections() {
	if c.httpc != nil {
		c.httpc.CloseIdleConnections()
	}
}

// defaultMaxRetries is higher than the SDK default (2) so transient 429s —
// common when one token is shared by several sessions — recover
// transparently. The SDK uses exponential backoff and honors Retry-After.
const defaultMaxRetries = 5

// ClientOption adjusts a Client at construction.
type ClientOption func(*clientConfig)

type clientConfig struct {
	request []option.RequestOption
	list    []option.RequestOption
	prepare func(*anthropic.BetaMessageNewParams)
}

// WithRequestOptions adds SDK request options to every call the client makes
// (headers, say).
func WithRequestOptions(opts ...option.RequestOption) ClientOption {
	return func(c *clientConfig) { c.request = append(c.request, opts...) }
}

// WithListOptions adds SDK request options to model listing only.
func WithListOptions(opts ...option.RequestOption) ClientOption {
	return func(c *clientConfig) { c.list = append(c.list, opts...) }
}

// WithPrepare sets a function applied to every Messages request just before
// it is sent — after the default betas, before cache breakpoints — for a host
// that needs requests shaped a particular way.
func WithPrepare(fn func(*anthropic.BetaMessageNewParams)) ClientOption {
	return func(c *clientConfig) { c.prepare = fn }
}

// New builds a Client from a credential and an optional base URL ("" is the
// Anthropic production API).
func New(cred Credential, baseURL string, tuning Tuning, options ...ClientOption) *Client {
	var cfg clientConfig
	for _, o := range options {
		o(&cfg)
	}
	httpc := newHTTPClient()
	opts := []option.RequestOption{
		option.WithMaxRetries(tuning.maxRetries()),
		option.WithHTTPClient(httpc),
	}
	if cred.IsOAuth() {
		opts = append(opts, option.WithAuthToken(cred.AuthToken))
	} else {
		opts = append(opts, option.WithAPIKey(cred.APIKey))
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	opts = append(opts, cfg.request...)
	return &Client{sdk: anthropic.NewClient(opts...), cred: cred, tuning: tuning, httpc: httpc,
		prepare: cfg.prepare, listOpts: cfg.list, baseURL: baseURL}
}

// IsOAuth reports whether this client authenticates via OAuth bearer token.
func (c *Client) IsOAuth() bool { return c.cred.IsOAuth() }
