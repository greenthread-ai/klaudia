package anthropicauth

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

	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/pkg/api"
)

// keychainService is the macOS Keychain service name Claude Code stores the
// OAuth session under (mc("-credentials") in 05-app-core.js with the default
// empty OAUTH_FILE_SUFFIX).
const keychainService = "Claude Code-credentials"

// ResolveCredential resolves the credential for the Anthropic API, first hit
// wins:
//
//  1. ANTHROPIC_API_KEY             → x-api-key
//  2. ANTHROPIC_AUTH_TOKEN          → Bearer (explicit override)
//  3. klaudia --login store         → x-api-key
//     (credentials.json under the config root)
//  4. Claude Code's login           → Bearer (OAuth) or x-api-key
//     (the macOS Keychain, or ~/.claude/.credentials.json elsewhere)
//
// 1-3 are Klaudia's own configuration; 4 borrows a Claude Code session and
// never overrides them. (From PR #100.)
func ResolveCredential() (api.Credential, error) {
	if k := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); k != "" {
		return api.Credential{APIKey: k}, nil
	}
	if t := strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN")); t != "" {
		return api.Credential{AuthToken: t}, nil
	}
	if cred, ok := loginCredential(); ok {
		return cred, nil
	}
	if cred, ok := claudeCodeCredential(); ok {
		return cred, nil
	}
	return api.Credential{}, fmt.Errorf(`Klaudia needs credentials before it can start.

Choose one:
  1. Anthropic API key:
     export ANTHROPIC_API_KEY="sk-ant-..."
     # or store it once: klaudia --login

  2. An existing Claude Code login:
     claude
     # sign in there, then run klaudia again
     # (read from the macOS Keychain, or ~/.claude/.credentials.json)

  3. An OpenAI-compatible provider:
     klaudia --create-config=global   # writes ~/.klaudia/config.toml
     # swap provider = "anthropic" for its commented OpenAI-compatible block,
     # export the env var its apiKeyEnv names, then run klaudia

A provider and its key belong in ~/.klaudia/config.toml: a project's
./.klaudia/config.toml cannot set them.

Tip: after setup, run /doctor inside Klaudia to verify auth and environment status.`)
}

// claudeCodeCreds is the JSON Claude Code stores for its login — in the macOS
// Keychain entry, and in ~/.claude/.credentials.json elsewhere — and the
// shape of Klaudia's own login store. Any field may be absent.
type claudeCodeCreds struct {
	APIKey        string `json:"apiKey"`
	ClaudeAIOAuth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

// parseCreds reads stored credential JSON. An OAuth session is preferred over
// a stored key, as a subscription login is Claude Code's primary credential.
func parseCreds(raw []byte) (claudeCodeCreds, api.Credential, bool) {
	var c claudeCodeCreds
	if json.Unmarshal(raw, &c) != nil {
		return c, api.Credential{}, false
	}
	if t := strings.TrimSpace(c.ClaudeAIOAuth.AccessToken); t != "" {
		return c, api.Credential{AuthToken: t}, true
	}
	if k := strings.TrimSpace(c.APIKey); k != "" {
		return c, api.Credential{APIKey: k}, true
	}
	return c, api.Credential{}, false
}

// oauthExpired reports whether the OAuth session is at or past expiry, with
// a minute's skew.
func (c claudeCodeCreds) oauthExpired() bool {
	e := c.ClaudeAIOAuth.ExpiresAt
	return e > 0 && time.Now().UnixMilli() > e-60_000
}

// claudeCodeCredential reads Claude Code's login for this platform.
func claudeCodeCredential() (api.Credential, bool) {
	if runtime.GOOS == "darwin" {
		return claudeCodeFromKeychain()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return api.Credential{}, false
	}
	return claudeCodeFromFile(filepath.Join(home, ".claude", ".credentials.json"))
}

// claudeCodeFromFile reads Claude Code's credentials file. An expired OAuth
// session with a refresh token is refreshed and the rotated tokens written
// back — refresh tokens rotate, so not writing back would leave Claude Code
// holding a dead one. The write is atomic (a temp file renamed over), keeps
// 0600 and every field Klaudia does not know, so a concurrent write by Claude
// Code can never leave its file half-written. Any failure falls back to the
// stored token and lets the API report the auth error.
func claudeCodeFromFile(path string) (api.Credential, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return api.Credential{}, false
	}
	c, cred, ok := parseCreds(raw)
	if !ok {
		return api.Credential{}, false
	}
	if cred.AuthToken != "" && c.oauthExpired() && c.ClaudeAIOAuth.RefreshToken != "" {
		if t, rerr := refreshOAuth(context.Background(), c.ClaudeAIOAuth.RefreshToken); rerr == nil {
			if updated, merr := mergeOAuthPayload(raw, t); merr == nil {
				_ = writeFileAtomic(path, updated, 0o600)
			}
			return api.Credential{AuthToken: t.AccessToken}, true
		}
	}
	return cred, true
}

// claudeCodeFromKeychain reads Claude Code's login from the macOS Keychain via
// `security find-generic-password`, refreshing an expired OAuth session and
// writing the new tokens back.
func claudeCodeFromKeychain() (api.Credential, bool) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", keychainAccount(), "-w", "-s", keychainService).Output()
	if err != nil {
		return api.Credential{}, false
	}
	raw := []byte(strings.TrimSpace(string(out)))
	c, cred, ok := parseCreds(raw)
	if !ok {
		return api.Credential{}, false
	}
	// Best effort: on any failure, fall back to the existing (possibly stale)
	// token and let the API surface a clear auth error.
	if cred.AuthToken != "" && c.oauthExpired() && c.ClaudeAIOAuth.RefreshToken != "" {
		if t, rerr := refreshOAuth(context.Background(), c.ClaudeAIOAuth.RefreshToken); rerr == nil {
			_ = writeKeychainOAuth(raw, t) // preserve unknown fields; ignore write error
			return api.Credential{AuthToken: t.AccessToken}, true
		}
	}
	return cred, true
}

// LoginPath is Klaudia's own credential store: credentials.json under the
// config root (~/.klaudia, or KLAUDIA_CONFIG_DIR), written by --login.
func LoginPath() string { return filepath.Join(session.ConfigRoot(), "credentials.json") }

// loginCredential reads the --login store.
func loginCredential() (api.Credential, bool) {
	raw, err := os.ReadFile(LoginPath())
	if err != nil {
		return api.Credential{}, false
	}
	_, cred, ok := parseCreds(raw)
	return cred, ok
}

// SaveLoginKey stores an API key in the --login store (0600 in a 0700
// directory) and returns the path.
func SaveLoginKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("no API key given")
	}
	path := LoginPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(map[string]string{"apiKey": key}, "", "  ")
	if err != nil {
		return "", err
	}
	return path, writeFileAtomic(path, append(data, '\n'), 0o600)
}

// writeFileAtomic replaces path with data: written to a temp file in the same
// directory, then renamed over, so a reader sees the old file or the new one
// and never half of either.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once renamed
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
