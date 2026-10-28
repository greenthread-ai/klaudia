package anthropicauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// isolate points the config root and HOME at temp dirs and clears the env
// credentials, so only what a test writes is found.
func isolate(t *testing.T) (configRoot, home string) {
	t.Helper()
	configRoot, home = t.TempDir(), t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", configRoot)
	t.Setenv("HOME", home)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	return configRoot, home
}

// The --login store is 0600, read back, and outranked only by the env vars.
func TestLoginStore(t *testing.T) {
	isolate(t)
	path, err := SaveLoginKey("  sk-ant-stored  ")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("credentials file mode %v, want 0600", st.Mode().Perm())
	}
	if cred, ok := loginCredential(); !ok || cred.APIKey != "sk-ant-stored" {
		t.Fatalf("loginCredential = %+v, %v", cred, ok)
	}
	if cred, err := ResolveCredential(); err != nil || cred.APIKey != "sk-ant-stored" {
		t.Errorf("ResolveCredential = %+v, %v; want the stored key", cred, err)
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-env")
	if cred, _ := ResolveCredential(); cred.APIKey != "sk-ant-env" {
		t.Errorf("the env key should outrank the store, got %+v", cred)
	}
	if _, err := SaveLoginKey("   "); err == nil {
		t.Error("an empty key was stored")
	}
}

func TestParseCredsPrefersOAuth(t *testing.T) {
	_, cred, ok := parseCreds([]byte(`{"apiKey":"k","claudeAiOauth":{"accessToken":"tok"}}`))
	if !ok || cred.AuthToken != "tok" || cred.APIKey != "" {
		t.Errorf("got %+v, %v; want the OAuth token", cred, ok)
	}
	if _, cred, ok := parseCreds([]byte(`{"apiKey":"k"}`)); !ok || cred.APIKey != "k" {
		t.Errorf("got %+v, %v; want the key", cred, ok)
	}
	for _, raw := range []string{`not json`, `{}`, `{"claudeAiOauth":{"accessToken":"  "}}`} {
		if _, _, ok := parseCreds([]byte(raw)); ok {
			t.Errorf("parseCreds(%s) = ok", raw)
		}
	}
}

// Claude Code's credentials file: a current token is used as is; an expired
// one is refreshed and the rotated tokens written back atomically, 0600,
// keeping every field Klaudia does not know.
func TestClaudeCodeCredentialsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	future := time.Now().Add(time.Hour).UnixMilli()
	os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"live","refreshToken":"r","expiresAt":`+itoa(future)+`}}`), 0o600)
	if cred, ok := claudeCodeFromFile(path); !ok || cred.AuthToken != "live" {
		t.Fatalf("current token: %+v, %v", cred, ok)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"fresh","refresh_token":"r2","expires_in":3600}`))
	}))
	defer server.Close()
	old := oauthTokenURL
	oauthTokenURL = server.URL
	defer func() { oauthTokenURL = old }()

	past := time.Now().Add(-time.Hour).UnixMilli()
	os.WriteFile(path, []byte(`{"mcpOAuth":{"keep":"me"},"claudeAiOauth":{"accessToken":"stale","refreshToken":"r","expiresAt":`+itoa(past)+`,"scopes":["x"]}}`), 0o600)
	cred, ok := claudeCodeFromFile(path)
	if !ok || cred.AuthToken != "fresh" {
		t.Fatalf("expired token: %+v, %v; want the refreshed one", cred, ok)
	}
	raw, _ := os.ReadFile(path)
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("written back unreadable: %v: %s", err, raw)
	}
	oauth := back["claudeAiOauth"].(map[string]any)
	if oauth["accessToken"] != "fresh" || oauth["refreshToken"] != "r2" || back["mcpOAuth"] == nil || oauth["scopes"] == nil {
		t.Errorf("written back = %s", raw)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v after write-back, want 0600", st.Mode().Perm())
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".*tmp-*")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
	if _, ok := claudeCodeFromFile(filepath.Join(dir, "missing.json")); ok {
		t.Error("a missing file produced a credential")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
