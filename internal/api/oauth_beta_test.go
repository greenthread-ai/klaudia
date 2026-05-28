package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TestNewGatesClaudeCodeImpersonationByCredential confirms the Claude-Code
// fingerprint (User-Agent prefix + X-Stainless-Lang: js) is only applied when
// the credential is OAuth. API-key callers have their own Anthropic billing
// relationship and have no reason to mask the Go SDK identity — masking would
// just make their requests harder to identify in Anthropic-side logs.
func TestNewGatesClaudeCodeImpersonationByCredential(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")

	var (
		mu      sync.Mutex
		headers http.Header
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		headers = r.Header.Clone()
		mu.Unlock()
		// We don't need a valid stream — failing fast still lets us inspect the
		// request the SDK actually sent.
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"test"}}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	for _, tc := range []struct {
		name           string
		cred           Credential
		wantClaudeCLI  bool
		wantStainless  string // expected X-Stainless-Lang, "" means don't care
		dontWantPrefix string // forbid this User-Agent prefix
	}{
		{
			name:          "oauth",
			cred:          Credential{AuthToken: "oauth-token"},
			wantClaudeCLI: true,
			wantStainless: "js",
		},
		{
			name:           "api-key",
			cred:           Credential{APIKey: "sk-..."},
			wantClaudeCLI:  false,
			dontWantPrefix: "claude-cli/",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			headers = nil
			mu.Unlock()

			c := New(tc.cred, server.URL)
			_, _ = c.StreamTurn(context.Background(), anthropic.BetaMessageNewParams{
				Model:     anthropic.Model(DefaultModel),
				MaxTokens: 16,
				Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
			}, StreamSink{})

			mu.Lock()
			h := headers
			mu.Unlock()
			if h == nil {
				t.Fatal("server never received a request")
			}
			ua := h.Get("User-Agent")
			if tc.wantClaudeCLI && !strings.HasPrefix(ua, "claude-cli/") {
				t.Errorf("OAuth client UA = %q, want claude-cli/ prefix", ua)
			}
			if tc.dontWantPrefix != "" && strings.HasPrefix(ua, tc.dontWantPrefix) {
				t.Errorf("API-key client UA = %q, must not start with %q (keeps Go SDK identity)", ua, tc.dontWantPrefix)
			}
			if tc.wantStainless != "" {
				if got := h.Get("X-Stainless-Lang"); got != tc.wantStainless {
					t.Errorf("X-Stainless-Lang = %q, want %q", got, tc.wantStainless)
				}
			}
			// API-key path must keep the Go SDK's native Stainless fingerprint —
			// not "js". (We don't pin the exact value; the SDK owns it.)
			if tc.name == "api-key" {
				if got := h.Get("X-Stainless-Lang"); got == "js" {
					t.Errorf("API-key client should keep Go SDK X-Stainless-Lang, got %q (impersonation leaked)", got)
				}
			}
		})
	}
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
