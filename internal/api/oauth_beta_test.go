package api

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestAugmentBetasAddsOAuthForOAuthCredential(t *testing.T) {
	// OAuth credential: oauth-2025-04-20 must be appended so Anthropic doesn't
	// treat the same token as external/throttled usage (Claude Code itself
	// always sends this when on OAuth — `if (Y7()) q.push(BZ)`).
	c := &Client{cred: Credential{AuthToken: "fake-oauth"}}
	got := c.augmentBetas(DefaultBetas)
	if !containsBeta(got, OAuthBeta) {
		t.Errorf("OAuth credential should add %q; got %v", OAuthBeta, got)
	}
	// Idempotent: calling again shouldn't duplicate.
	got = c.augmentBetas(got)
	count := 0
	for _, b := range got {
		if b == OAuthBeta {
			count++
		}
	}
	if count != 1 {
		t.Errorf("OAuthBeta appears %d times after re-augmenting, want 1", count)
	}
}

func TestAugmentBetasNoOpForAPIKey(t *testing.T) {
	// API-key credential: NOT in the OAuth quota bucket, so the OAuth beta
	// would just be misleading. Don't add it.
	c := &Client{cred: Credential{APIKey: "sk-..."}}
	got := c.augmentBetas(DefaultBetas)
	if containsBeta(got, OAuthBeta) {
		t.Errorf("API-key credential should not get %q; got %v", OAuthBeta, got)
	}
}

func containsBeta(list []anthropic.AnthropicBeta, want anthropic.AnthropicBeta) bool {
	for _, b := range list {
		if b == want {
			return true
		}
	}
	return false
}

func TestUserAgentMatchesClaudeCodeShape(t *testing.T) {
	// Anthropic rate-limits OAuth tokens by client fingerprint — User-Agent +
	// X-Stainless-*. A `claude-cli/…` prefix is the signal we need; the rest
	// of the string identifies us as klaudia so the request is honest.
	ua := userAgent()
	if !strings.HasPrefix(ua, "claude-cli/") {
		t.Errorf("UA prefix must be claude-cli/...; got %q", ua)
	}
	if !strings.Contains(ua, "klaudia/") {
		t.Errorf("UA should still identify us as klaudia in the entrypoint segment; got %q", ua)
	}
	if !strings.Contains(ua, "external") {
		t.Errorf("UA should carry the (external, …) entrypoint marker; got %q", ua)
	}
}
