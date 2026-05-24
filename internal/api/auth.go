package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"time"
)

// Credential is the resolved auth used to talk to the Anthropic API.
//
// Exactly one of APIKey / AuthToken is set. APIKey maps to the `x-api-key`
// header; AuthToken is an OAuth bearer token (Authorization: Bearer …), used
// when the user is signed in via a Claude Code OAuth session.
type Credential struct {
	APIKey    string
	AuthToken string
}

// IsOAuth reports whether this credential is an OAuth bearer token.
func (c Credential) IsOAuth() bool { return c.AuthToken != "" }

// keychainService is the macOS Keychain service name Claude Code stores the
// OAuth session under (mc("-credentials") in 05-app-core.js with the default
// empty OAUTH_FILE_SUFFIX).
const keychainService = "Claude Code-credentials"

// ResolveCredential mirrors the JS auth precedence (createApiClient,
// 05-app-core.js:56018): an explicit API key env var wins, otherwise fall back
// to the Claude Code OAuth session.
//
// Precedence:
//  1. ANTHROPIC_API_KEY            → x-api-key
//  2. ANTHROPIC_AUTH_TOKEN         → Bearer (explicit override)
//  3. macOS Keychain OAuth session → Bearer
func ResolveCredential() (Credential, error) {
	if k := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); k != "" {
		return Credential{APIKey: k}, nil
	}
	if t := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")); t != "" {
		return Credential{AuthToken: t}, nil
	}
	if tok, ok := oauthTokenFromKeychain(); ok {
		return Credential{AuthToken: tok}, nil
	}
	return Credential{}, fmt.Errorf(`Klaudia needs credentials before it can start.

Choose one:
  1. Anthropic API key:
     export ANTHROPIC_API_KEY="sk-ant-..."
     klaudia

  2. Existing Claude Code login on macOS:
     claude
     # sign in there, then run klaudia again

  3. OpenAI-compatible provider:
     create .klaudia/config.json with provider/baseURL/model/apiKeyEnv,
     export that API key env var, then run klaudia again.

Tip: after setup, run /doctor inside Klaudia to verify auth and environment status.`)
}

// keychainPayload is the JSON shape stored in the Keychain entry.
type keychainPayload struct {
	ClaudeAIOAuth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// oauthTokenFromKeychain reads the Claude Code OAuth access token from the
// macOS Keychain via `security find-generic-password`, matching the JS
// keychain provider (05-app-core.js:99000). Returns ("", false) on any
// non-darwin platform or when no entry exists. When the token is expired and a
// refresh token is present, it refreshes and writes the new token back.
func oauthTokenFromKeychain() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	out, err := exec.Command("security", "find-generic-password",
		"-a", keychainAccount(), "-w", "-s", keychainService).Output()
	if err != nil {
		return "", false
	}
	raw := []byte(strings.TrimSpace(string(out)))
	var p keychainPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", false
	}
	if p.ClaudeAIOAuth.AccessToken == "" {
		return "", false
	}

	// Refresh if expired (60s skew) and we have a refresh token. Best effort:
	// on any failure, fall back to the existing (possibly stale) token and let
	// the API surface a clear auth error.
	expired := p.ClaudeAIOAuth.ExpiresAt > 0 && time.Now().UnixMilli() > p.ClaudeAIOAuth.ExpiresAt-60_000
	if expired && p.ClaudeAIOAuth.RefreshToken != "" {
		if t, rerr := refreshOAuth(context.Background(), p.ClaudeAIOAuth.RefreshToken); rerr == nil {
			_ = writeKeychainOAuth(raw, t) // preserve unknown fields; ignore write error
			return t.AccessToken, true
		}
	}
	return p.ClaudeAIOAuth.AccessToken, true
}

// writeKeychainOAuth updates the claudeAiOauth access/refresh/expiry fields in
// the stored JSON (preserving every other field) and writes it back to the
// Keychain via `security add-generic-password -U`.
func writeKeychainOAuth(rawCurrent []byte, t refreshedTokens) error {
	updated, err := mergeOAuthPayload(rawCurrent, t)
	if err != nil {
		return err
	}
	return exec.Command("security", "add-generic-password", "-U",
		"-a", keychainAccount(), "-s", keychainService, "-w", string(updated)).Run()
}

// mergeOAuthPayload updates only the claudeAiOauth access/refresh/expiry fields
// in the stored JSON, preserving every other field, and returns the new JSON.
func mergeOAuthPayload(rawCurrent []byte, t refreshedTokens) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(rawCurrent, &m); err != nil {
		return nil, err
	}
	oauth, ok := m["claudeAiOauth"].(map[string]any)
	if !ok {
		oauth = map[string]any{}
	}
	oauth["accessToken"] = t.AccessToken
	oauth["refreshToken"] = t.RefreshToken
	oauth["expiresAt"] = t.ExpiresAt
	m["claudeAiOauth"] = oauth
	return json.Marshal(m)
}

// keychainAccount mirrors lW6() (05-app-core.js:98961): the keychain account is
// the current username.
func keychainAccount() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "claude-code-user"
}
