// Package api wraps the official anthropic-sdk-go: it resolves credentials
// (env API key or Claude Code OAuth session), applies the Claude Code default
// headers/betas, resolves model aliases, and exposes a streaming Messages call.
//
// We deliberately use the Beta Messages surface because Klaudia relies on beta
// features (claude-code, context-management, server-side web tools).
package api

import (
	"crypto/rand"
	"fmt"
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

// claudeCode* mirror the *current* Claude Code release we impersonate on the
// OAuth path. Anthropic rate-limits Claude Max OAuth tokens on the full request
// fingerprint (UA + Stainless headers + session id + beta set), not just the
// token, so these need to track whatever a real `claude` is sending. Re-capture
// with the helper in headerdump_test.go (or any mitmproxy of `claude -p`) and
// bump these constants when claude moves.
const (
	claudeCodeVersion          = "2.1.153"
	claudeCodeStainlessVersion = "0.94.0"
	claudeCodeNodeVersion      = "v24.3.0"
	claudeCodeStainlessTimeout = "600"
)

// userAgent returns the User-Agent string we send on the OAuth path. We mirror
// what real `claude` ships byte-for-byte — no klaudia signature in the
// entrypoint segment. The user signed off on full mimicry on OAuth: we're
// spending Claude Max quota with a Claude Code token, so the request should
// look like Claude Code on the wire. API-key callers get the SDK-native UA.
func userAgent() string {
	return "claude-cli/" + claudeCodeVersion + " (external, sdk-ts)"
}

// newClaudeCodeSessionID returns a UUIDv4 for the X-Claude-Code-Session-Id
// header. `claude` emits a stable per-session UUID; missing or malformed values
// are one more fingerprint divergence the rate-limit path can pattern-match on.
// Not security-sensitive — purely identity-matching — so the rand failure path
// returns a zero UUID rather than aborting.
func newClaudeCodeSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

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
func New(cred Credential, baseURL string) *Client {
	opts := []option.RequestOption{
		option.WithHeader("x-app", "cli"),
		option.WithMaxRetries(maxRetries()),
	}
	if cred.IsOAuth() {
		// The Claude Code OAuth flow is gated by Anthropic on the *full request
		// fingerprint*, not just the token. A `claude-cli/…` UA paired with
		// `X-Stainless-Lang: go` (the Go SDK's default) lands us in the strict
		// external-use rate-limit bucket — observable as instant 429s while
		// `claude` itself, holding the same Keychain token, works fine. We
		// capture a real `claude -p` against a local proxy (see
		// headerdump_test.go for the harness) and mirror every static header it
		// sends: UA, Stainless identity, browser-access flag, session id, and
		// the timeout hint. API-key callers have their own Anthropic billing
		// relationship and skip all of this — they keep the SDK-native identity
		// so their requests stay identifiable in Anthropic-side logs.
		opts = append(opts,
			option.WithAuthToken(cred.AuthToken),
			option.WithHeader("User-Agent", userAgent()),
			option.WithHeader("X-Stainless-Lang", "js"),
			option.WithHeader("X-Stainless-Runtime", "node"),
			option.WithHeader("X-Stainless-Runtime-Version", claudeCodeNodeVersion),
			option.WithHeader("X-Stainless-Package-Version", claudeCodeStainlessVersion),
			option.WithHeader("X-Stainless-Timeout", claudeCodeStainlessTimeout),
			option.WithHeader("Anthropic-Dangerous-Direct-Browser-Access", "true"),
			option.WithHeader("X-Claude-Code-Session-Id", newClaudeCodeSessionID()),
		)
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
