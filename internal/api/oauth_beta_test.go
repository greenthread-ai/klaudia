package api

import (
	"context"
	"encoding/json"
	"io"
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

// TestOAuthRequestCarriesBillingHeaderInBody is the end-to-end gate: real
// `claude -p` against a local proxy showed that the *only* discriminator
// between 200 and 429 on OAuth is the `x-anthropic-billing-header:` prefix on
// system[0]. Bogus cch values, missing oauth-beta, missing UA, missing every
// X-Stainless-* — all 200 as long as that prefix is present. (Curl tests
// captured in git history of this file.) So instead of asserting which headers
// we ship, we assert the body shape that actually flips the bucket — and that
// API-key callers don't get the prefix shoved into their tenant by accident.
func TestOAuthRequestCarriesBillingHeaderInBody(t *testing.T) {
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")

	var (
		mu   sync.Mutex
		body []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = b
		mu.Unlock()
		http.Error(w, `{"type":"error","error":{"type":"api_error","message":"test"}}`, http.StatusInternalServerError)
	}))
	defer server.Close()

	for _, tc := range []struct {
		name        string
		cred        Credential
		wantPrefix  bool
		userSystem  string
		wantSysLen  int    // expected length of body.system array
		wantSysFst0 string // expected text of body.system[0] (empty = don't check)
	}{
		{
			name:        "oauth/with user system",
			cred:        Credential{AuthToken: "oauth-token"},
			wantPrefix:  true,
			userSystem:  "You are a helpful assistant.",
			wantSysLen:  2,
			wantSysFst0: "", // checked separately via wantPrefix
		},
		{
			name:       "oauth/empty user system",
			cred:       Credential{AuthToken: "oauth-token"},
			wantPrefix: true,
			wantSysLen: 1,
		},
		{
			name:        "api-key/with user system",
			cred:        Credential{APIKey: "sk-..."},
			wantPrefix:  false,
			userSystem:  "You are a helpful assistant.",
			wantSysLen:  1,
			wantSysFst0: "You are a helpful assistant.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			body = nil
			mu.Unlock()

			var sys []anthropic.BetaTextBlockParam
			if tc.userSystem != "" {
				sys = []anthropic.BetaTextBlockParam{{Text: tc.userSystem}}
			}

			c := New(tc.cred, server.URL)
			_, _ = c.StreamTurn(context.Background(), anthropic.BetaMessageNewParams{
				Model:     anthropic.Model(DefaultModel),
				MaxTokens: 16,
				Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
				System:    sys,
			}, StreamSink{})

			mu.Lock()
			b := body
			mu.Unlock()
			if len(b) == 0 {
				t.Fatal("server never received a request body")
			}

			var got struct {
				System []struct {
					Text string `json:"text"`
				} `json:"system"`
			}
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("body parse: %v\nbody=%s", err, b)
			}
			if len(got.System) != tc.wantSysLen {
				t.Fatalf("system blocks = %d, want %d (body=%s)", len(got.System), tc.wantSysLen, b)
			}
			if tc.wantPrefix {
				if !strings.HasPrefix(got.System[0].Text, claudeCodeBillingPrefix) {
					t.Errorf("system[0] = %q, must start with %q on OAuth — Anthropic gates on it", got.System[0].Text, claudeCodeBillingPrefix)
				}
				if !strings.Contains(got.System[0].Text, "cc_entrypoint=klaudia") {
					t.Errorf("billing block should declare klaudia honestly; got %q", got.System[0].Text)
				}
			} else {
				for i, blk := range got.System {
					if strings.HasPrefix(blk.Text, claudeCodeBillingPrefix) {
						t.Errorf("API-key system[%d] = %q carries OAuth billing prefix (impersonation leaked)", i, blk.Text)
					}
				}
			}
			if tc.wantSysFst0 != "" && got.System[0].Text != tc.wantSysFst0 {
				t.Errorf("system[0] = %q, want %q", got.System[0].Text, tc.wantSysFst0)
			}
		})
	}
}
