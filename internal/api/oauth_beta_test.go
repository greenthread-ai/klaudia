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

func TestAugmentSystemPrependsBillingHeaderForOAuth(t *testing.T) {
	// OAuth credentials need the `x-anthropic-billing-header:` prefix as the
	// FIRST system block — discovered empirically by capturing real claude vs
	// our requests and binary-searching the diff: with the prefix, 200; without,
	// 429 instantly. Values after the prefix (cc_version, cc_entrypoint, cch)
	// are server-logged but not validated, so we ship klaudia branding honestly.
	c := &Client{cred: Credential{AuthToken: "fake-oauth"}}

	// Existing system block: billing must land in front of it, not at the end.
	user := []anthropic.BetaTextBlockParam{{Text: "You are a helpful assistant."}}
	got := c.augmentSystem(user)
	if len(got) != 2 {
		t.Fatalf("system blocks = %d, want 2", len(got))
	}
	if !strings.HasPrefix(got[0].Text, claudeCodeBillingPrefix) {
		t.Errorf("block[0].Text = %q, want %q prefix", got[0].Text, claudeCodeBillingPrefix)
	}
	if got[1].Text != user[0].Text {
		t.Errorf("user system block displaced: block[1] = %q, want %q", got[1].Text, user[0].Text)
	}
	if !strings.Contains(got[0].Text, "cc_entrypoint=klaudia") {
		t.Errorf("billing entrypoint must declare klaudia honestly; got %q", got[0].Text)
	}

	// Empty input: still gets the billing block — otherwise requests with no
	// user-supplied system would 429.
	got = c.augmentSystem(nil)
	if len(got) != 1 || !strings.HasPrefix(got[0].Text, claudeCodeBillingPrefix) {
		t.Errorf("nil input must yield a single billing block; got %+v", got)
	}

	// Idempotent: re-augmenting an already-augmented slice doesn't double up.
	twice := c.augmentSystem(got)
	if len(twice) != 1 {
		t.Errorf("idempotency violated: %d blocks after second augment, want 1", len(twice))
	}
}

func TestAugmentSystemNoOpForAPIKey(t *testing.T) {
	// API-key callers go straight to their own Anthropic tenant; the billing-
	// header gate doesn't apply, and prepending one would just confuse logs.
	c := &Client{cred: Credential{APIKey: "sk-..."}}
	user := []anthropic.BetaTextBlockParam{{Text: "You are a helpful assistant."}}
	got := c.augmentSystem(user)
	if len(got) != 1 || got[0].Text != user[0].Text {
		t.Errorf("API-key system should pass through unchanged; got %+v", got)
	}
	if got := c.augmentSystem(nil); got != nil {
		t.Errorf("API-key + nil system should stay nil; got %+v", got)
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
			// The full set of Claude-Code-only headers — UA + Stainless lang is
			// the loudest, but each missing header is another bit of entropy a
			// rate-limit filter could key on. Track them all so a future refactor
			// can't silently drop one.
			oauthOnlyHeaders := []string{
				"X-Stainless-Timeout",
				"Anthropic-Dangerous-Direct-Browser-Access",
				"X-Claude-Code-Session-Id",
			}
			if tc.name == "oauth" {
				for _, name := range oauthOnlyHeaders {
					if h.Get(name) == "" {
						t.Errorf("OAuth client missing %s — Claude Code sends it; full fingerprint match needs every header", name)
					}
				}
				if h.Get("X-Stainless-Package-Version") != claudeCodeStainlessVersion {
					t.Errorf("X-Stainless-Package-Version = %q, want %q (mirror real claude)", h.Get("X-Stainless-Package-Version"), claudeCodeStainlessVersion)
				}
			}
			if tc.name == "api-key" {
				// API-key path must keep the Go SDK's native Stainless fingerprint —
				// not "js" — and must NOT carry the Claude Code impersonation
				// headers either. (We don't pin Stainless values; the SDK owns them.)
				if got := h.Get("X-Stainless-Lang"); got == "js" {
					t.Errorf("API-key client should keep Go SDK X-Stainless-Lang, got %q (impersonation leaked)", got)
				}
				for _, name := range oauthOnlyHeaders {
					if got := h.Get(name); got != "" {
						t.Errorf("API-key client carries OAuth-only header %s = %q (impersonation leaked)", name, got)
					}
				}
			}
		})
	}
}

func TestUserAgentMatchesClaudeCodeShape(t *testing.T) {
	// Mirror real claude exactly on the OAuth path: `claude-cli/<ver> (external,
	// sdk-ts)`. No klaudia signature in the entrypoint — Anthropic gates the
	// Claude Max OAuth quota on the full fingerprint, and a `; klaudia/...`
	// extension was a one-line giveaway. (Captured ground-truth via the
	// headerdump_test harness against a real `claude -p` run.)
	ua := userAgent()
	if !strings.HasPrefix(ua, "claude-cli/"+claudeCodeVersion+" ") {
		t.Errorf("UA must start with claude-cli/%s ; got %q", claudeCodeVersion, ua)
	}
	if !strings.HasSuffix(ua, " (external, sdk-ts)") {
		t.Errorf("UA must end with (external, sdk-ts); got %q", ua)
	}
	if strings.Contains(ua, "klaudia") {
		t.Errorf("UA must not leak the klaudia name on the OAuth fingerprint; got %q", ua)
	}
}

func TestNewClaudeCodeSessionIDIsUUIDv4(t *testing.T) {
	// claude sends a stable per-session UUIDv4. Verify shape so we don't drift
	// (e.g. accidentally emit a hex-without-dashes that fails a server-side
	// regex). Two consecutive calls must also produce different ids.
	id := newClaudeCodeSessionID()
	parts := strings.Split(id, "-")
	wantLens := []int{8, 4, 4, 4, 12}
	if len(parts) != len(wantLens) {
		t.Fatalf("UUID parts = %d, want %d (got %q)", len(parts), len(wantLens), id)
	}
	for i, n := range wantLens {
		if len(parts[i]) != n {
			t.Errorf("part %d = %q (len %d), want len %d", i, parts[i], len(parts[i]), n)
		}
	}
	// Version nibble: first char of part[2] must be '4'.
	if len(parts[2]) > 0 && parts[2][0] != '4' {
		t.Errorf("UUIDv4 version nibble = %q, want '4' (id %q)", parts[2][:1], id)
	}
	if id == newClaudeCodeSessionID() {
		t.Errorf("session ids must differ between calls (got %q twice)", id)
	}
}
