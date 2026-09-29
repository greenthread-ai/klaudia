package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// isolatedAuthEnv points HOME and KLAUDIA_CONFIG_DIR at fresh temp dirs and
// clears the Anthropic auth env vars, so credential resolution reads only the
// fixtures a test writes. It returns (homeDir, configDir).
func isolatedAuthEnv(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	cfg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KLAUDIA_CONFIG_DIR", cfg)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	return home, cfg
}

func TestWriteLoginAPIKeyRoundTripAndPerms(t *testing.T) {
	_, cfg := isolatedAuthEnv(t)

	path, err := WriteLoginAPIKey("  sk-ant-written  ")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cfg, "credentials.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 600", perm)
	}

	cred, ok := loginCredential()
	if !ok {
		t.Fatal("loginCredential() ok = false, want true")
	}
	if cred.APIKey != "sk-ant-written" { // trimmed on write
		t.Errorf("APIKey = %q, want sk-ant-written", cred.APIKey)
	}
	if cred.IsOAuth() {
		t.Error("stored api key must not be an OAuth credential")
	}
}

func TestWriteLoginAPIKeyRejectsEmpty(t *testing.T) {
	isolatedAuthEnv(t)
	if _, err := WriteLoginAPIKey("   "); err == nil {
		t.Fatal("WriteLoginAPIKey(empty) err = nil, want error")
	}
}

func TestLoginCredentialMissingFile(t *testing.T) {
	isolatedAuthEnv(t)
	if _, ok := loginCredential(); ok {
		t.Fatal("loginCredential() ok = true with no file, want false")
	}
}

func TestResolveCredentialPrecedence(t *testing.T) {
	t.Run("ANTHROPIC_API_KEY wins over everything", func(t *testing.T) {
		home, _ := isolatedAuthEnv(t)
		t.Setenv("ANTHROPIC_API_KEY", "sk-env-key")
		if _, err := WriteLoginAPIKey("sk-login-key"); err != nil {
			t.Fatal(err)
		}
		writeClaudeCodeFile(t, home, `{"claudeAiOauth":{"accessToken":"oauth-tok"}}`)

		cred, err := ResolveCredential()
		if err != nil {
			t.Fatal(err)
		}
		if cred.APIKey != "sk-env-key" || cred.IsOAuth() {
			t.Errorf("cred = %+v, want APIKey sk-env-key", cred)
		}
	})

	t.Run("ANTHROPIC_AUTH_TOKEN beats login store", func(t *testing.T) {
		isolatedAuthEnv(t)
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "env-bearer")
		if _, err := WriteLoginAPIKey("sk-login-key"); err != nil {
			t.Fatal(err)
		}
		cred, err := ResolveCredential()
		if err != nil {
			t.Fatal(err)
		}
		if cred.AuthToken != "env-bearer" || !cred.IsOAuth() {
			t.Errorf("cred = %+v, want AuthToken env-bearer", cred)
		}
	})

	t.Run("login store beats Claude Code fallback", func(t *testing.T) {
		home, _ := isolatedAuthEnv(t)
		if _, err := WriteLoginAPIKey("sk-login-key"); err != nil {
			t.Fatal(err)
		}
		writeClaudeCodeFile(t, home, `{"claudeAiOauth":{"accessToken":"oauth-tok"}}`)

		cred, err := ResolveCredential()
		if err != nil {
			t.Fatal(err)
		}
		if cred.APIKey != "sk-login-key" || cred.IsOAuth() {
			t.Errorf("cred = %+v, want APIKey sk-login-key", cred)
		}
	})

	t.Run("Claude Code fallback used when nothing else set", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("darwin reads the Keychain, not ~/.claude/.credentials.json")
		}
		home, _ := isolatedAuthEnv(t)
		writeClaudeCodeFile(t, home, `{"claudeAiOauth":{"accessToken":"oauth-tok"}}`)

		cred, err := ResolveCredential()
		if err != nil {
			t.Fatal(err)
		}
		if cred.AuthToken != "oauth-tok" || !cred.IsOAuth() {
			t.Errorf("cred = %+v, want AuthToken oauth-tok", cred)
		}
	})

	t.Run("no credentials is an error", func(t *testing.T) {
		if runtime.GOOS == "darwin" {
			t.Skip("darwin may find a real Keychain session")
		}
		isolatedAuthEnv(t)
		if _, err := ResolveCredential(); err == nil {
			t.Fatal("ResolveCredential() err = nil, want error with no creds")
		}
	})
}

func writeClaudeCodeFile(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".credentials.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseClaudeCodeCreds(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantOK  bool
		wantKey string
		wantTok string
	}{
		{"oauth token", `{"claudeAiOauth":{"accessToken":"tok"}}`, true, "", "tok"},
		{"api key only", `{"apiKey":"sk-x"}`, true, "sk-x", ""},
		{"oauth preferred over apikey", `{"apiKey":"sk-x","claudeAiOauth":{"accessToken":"tok"}}`, true, "", "tok"},
		{"empty object", `{}`, false, "", ""},
		{"garbage", `not json`, false, "", ""},
		{"blank oauth falls through", `{"claudeAiOauth":{"accessToken":"   "}}`, false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cred, ok := parseClaudeCodeCreds([]byte(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if cred.APIKey != tc.wantKey {
				t.Errorf("APIKey = %q, want %q", cred.APIKey, tc.wantKey)
			}
			if cred.AuthToken != tc.wantTok {
				t.Errorf("AuthToken = %q, want %q", cred.AuthToken, tc.wantTok)
			}
		})
	}
}

func TestClaudeCodeFromFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("missing file", func(t *testing.T) {
		if _, ok := claudeCodeFromFile(filepath.Join(dir, "nope.json")); ok {
			t.Fatal("ok = true for missing file, want false")
		}
	})
	t.Run("empty path", func(t *testing.T) {
		if _, ok := claudeCodeFromFile(""); ok {
			t.Fatal("ok = true for empty path, want false")
		}
	})
	t.Run("valid oauth", func(t *testing.T) {
		p := filepath.Join(dir, "oauth.json")
		// expiresAt far in the future, so no refresh is attempted.
		body := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"tok","expiresAt":%d}}`, time.Now().Add(time.Hour).UnixMilli())
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cred, ok := claudeCodeFromFile(p)
		if !ok || cred.AuthToken != "tok" {
			t.Fatalf("cred = %+v ok = %v, want AuthToken tok", cred, ok)
		}
	})
	t.Run("api key", func(t *testing.T) {
		p := filepath.Join(dir, "apikey.json")
		if err := os.WriteFile(p, []byte(`{"apiKey":"sk-file"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		cred, ok := claudeCodeFromFile(p)
		if !ok || cred.APIKey != "sk-file" {
			t.Fatalf("cred = %+v ok = %v, want APIKey sk-file", cred, ok)
		}
	})
}

// TestClaudeCodeFromFileRefreshesExpiredOAuth verifies an expired OAuth session
// with a refresh token is refreshed and the rotated tokens written back (0600).
func TestClaudeCodeFromFileRefreshesExpiredOAuth(t *testing.T) {
	srv := oauthRefreshServer(t, `{"access_token":"fresh","refresh_token":"fresh-refresh","expires_in":3600}`)
	defer srv.close()

	p := filepath.Join(t.TempDir(), ".credentials.json")
	body := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"stale","refreshToken":"r0","expiresAt":%d},"keep":"me"}`,
		time.Now().Add(-time.Hour).UnixMilli())
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cred, ok := claudeCodeFromFile(p)
	if !ok || cred.AuthToken != "fresh" {
		t.Fatalf("cred = %+v ok = %v, want AuthToken fresh", cred, ok)
	}

	// The rotated tokens are persisted, preserving unknown fields.
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	oauth := m["claudeAiOauth"].(map[string]any)
	if oauth["accessToken"] != "fresh" || oauth["refreshToken"] != "fresh-refresh" {
		t.Errorf("tokens not rotated on disk: %v", oauth)
	}
	if m["keep"] != "me" {
		t.Errorf("dropped unknown field: %v", m)
	}
}

// oauthRefreshServer stands in for the OAuth token endpoint, returning body on
// POST. It repoints oauthTokenURL for the duration and restores it on close.
type stubServer struct{ close func() }

func oauthRefreshServer(t *testing.T, body string) stubServer {
	t.Helper()
	old := oauthTokenURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	oauthTokenURL = srv.URL
	return stubServer{close: func() {
		oauthTokenURL = old
		srv.Close()
	}}
}
