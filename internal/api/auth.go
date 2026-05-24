package api

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
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
	return Credential{}, fmt.Errorf("no credentials: set ANTHROPIC_API_KEY or sign in with Claude Code (OAuth session in the macOS Keychain)")
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
// non-darwin platform or when no entry exists.
//
// TODO(phase3): handle token refresh when ExpiresAt is in the past.
func oauthTokenFromKeychain() (string, bool) {
	if runtime.GOOS != "darwin" {
		return "", false
	}
	account := keychainAccount()
	out, err := exec.Command("security", "find-generic-password",
		"-a", account, "-w", "-s", keychainService).Output()
	if err != nil {
		return "", false
	}
	var p keychainPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &p); err != nil {
		return "", false
	}
	if p.ClaudeAIOAuth.AccessToken == "" {
		return "", false
	}
	return p.ClaudeAIOAuth.AccessToken, true
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
