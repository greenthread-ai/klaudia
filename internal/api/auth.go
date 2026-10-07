package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
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

// ResolveCredential resolves the auth used to talk to the Anthropic API,
// mirroring the JS auth precedence (createApiClient, 05-app-core.js:56018) and
// extending it with a `klaudia login` store and a Linux Claude Code fallback.
//
// Precedence (first hit wins):
//  1. ANTHROPIC_API_KEY               → x-api-key
//  2. ANTHROPIC_AUTH_TOKEN            → Bearer (explicit override)
//  3. klaudia login store             → x-api-key or Bearer
//     (~/.klaudia/credentials.json, honouring KLAUDIA_CONFIG_DIR)
//  4. Claude Code credentials         → Bearer (OAuth) or x-api-key
//     (macOS Keychain, or ~/.claude/.credentials.json on Linux/other)
//
// Steps 1–3 are explicit Klaudia configuration; step 4 is a borrowed Claude
// Code session, a convenience fallback that never overrides the former.
func ResolveCredential() (Credential, error) {
	if k := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); k != "" {
		return Credential{APIKey: k}, nil
	}
	if t := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")); t != "" {
		return Credential{AuthToken: t}, nil
	}
	if cred, ok := loginCredential(); ok {
		return cred, nil
	}
	if cred, ok := claudeCodeCredential(); ok {
		return cred, nil
	}
	return Credential{}, fmt.Errorf(`Klaudia needs credentials before it can start.

Choose one:
  1. Anthropic API key:
     export ANTHROPIC_API_KEY="sk-ant-..."
     klaudia
     # or store it once:  klaudia login   (prompts, writes ~/.klaudia/credentials.json)

  2. Existing Claude Code login:
     claude
     # sign in there, then run klaudia again
     # (uses the macOS Keychain, or ~/.claude/.credentials.json on Linux)

  3. OpenAI-compatible provider config:
     klaudia --create-config=global   # writes ~/.klaudia/config.toml
     # or: klaudia --create-config=local  # writes ./.klaudia/config.toml
     # edit provider/baseURL/model/apiKeyEnv, export that env var, then run klaudia

Global config (~/.klaudia/config.toml) is loaded automatically; local project config
(./.klaudia/config.toml) overlays it when present.

Tip: after setup, run /doctor inside Klaudia to verify auth and environment status.`)
}

// claudeCodeCreds is the JSON shape Claude Code stores for its login, in the
// macOS Keychain entry and in ~/.claude/.credentials.json on Linux. Any field
// may be absent; it is parsed defensively.
type claudeCodeCreds struct {
	// APIKey is a stored Anthropic API key, if Claude Code holds one.
	APIKey string `json:"apiKey"`
	// ClaudeAIOAuth holds an OAuth session from a Claude subscription login.
	ClaudeAIOAuth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// parseClaudeCodeCreds parses stored credential JSON into a Credential. An
// OAuth access token (Bearer) is preferred over a stored API key, matching how
// a subscription login is the primary Claude Code credential. ok is false when
// the JSON is unparseable or holds neither shape.
func parseClaudeCodeCreds(raw []byte) (creds claudeCodeCreds, cred Credential, ok bool) {
	if err := json.Unmarshal(raw, &creds); err != nil {
		return creds, Credential{}, false
	}
	if tok := strings.TrimSpace(creds.ClaudeAIOAuth.AccessToken); tok != "" {
		return creds, Credential{AuthToken: tok}, true
	}
	if k := strings.TrimSpace(creds.APIKey); k != "" {
		return creds, Credential{APIKey: k}, true
	}
	return creds, Credential{}, false
}

// oauthExpired reports whether an OAuth session is at or past expiry (60s skew).
func (c claudeCodeCreds) oauthExpired() bool {
	e := c.ClaudeAIOAuth.ExpiresAt
	return e > 0 && time.Now().UnixMilli() > e-60_000
}

// claudeCodeCredential reads the Claude Code login credential for the current
// platform: the macOS Keychain on darwin, else ~/.claude/.credentials.json.
func claudeCodeCredential() (Credential, bool) {
	if runtime.GOOS == "darwin" {
		return claudeCodeFromKeychain()
	}
	return claudeCodeFromFile(claudeCodeCredentialsPath())
}

// claudeCodeCredentialsPath is Claude Code's Linux credentials file. Returns ""
// when the home directory can't be determined.
func claudeCodeCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

// claudeCodeFromFile reads a Claude Code credentials JSON file. When it holds
// an expired OAuth session with a refresh token, it refreshes best-effort and
// writes the rotated tokens back (0600, preserving unknown fields); on any
// refresh/write failure it falls back to the stored token and lets the API
// surface a clear auth error.
func claudeCodeFromFile(path string) (Credential, bool) {
	if path == "" {
		return Credential{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Credential{}, false
	}
	creds, cred, ok := parseClaudeCodeCreds(raw)
	if !ok {
		return Credential{}, false
	}
	if cred.IsOAuth() && creds.oauthExpired() && creds.ClaudeAIOAuth.RefreshToken != "" {
		if t, rerr := refreshOAuth(context.Background(), creds.ClaudeAIOAuth.RefreshToken); rerr == nil {
			if updated, merr := mergeOAuthPayload(raw, t); merr == nil {
				_ = os.WriteFile(path, updated, 0o600)
			}
			return Credential{AuthToken: t.AccessToken}, true
		}
	}
	return cred, true
}

// claudeCodeFromKeychain reads the Claude Code login from the macOS Keychain
// via `security find-generic-password`, matching the JS keychain provider
// (05-app-core.js:99000). When the OAuth token is expired and a refresh token
// is present, it refreshes and writes the new token back.
func claudeCodeFromKeychain() (Credential, bool) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", keychainAccount(), "-w", "-s", keychainService).Output()
	if err != nil {
		return Credential{}, false
	}
	raw := []byte(strings.TrimSpace(string(out)))
	creds, cred, ok := parseClaudeCodeCreds(raw)
	if !ok {
		return Credential{}, false
	}
	if cred.IsOAuth() && creds.oauthExpired() && creds.ClaudeAIOAuth.RefreshToken != "" {
		if t, rerr := refreshOAuth(context.Background(), creds.ClaudeAIOAuth.RefreshToken); rerr == nil {
			_ = writeKeychainOAuth(raw, t) // preserve unknown fields; ignore write error
			return Credential{AuthToken: t.AccessToken}, true
		}
	}
	return cred, true
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
