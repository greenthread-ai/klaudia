package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/session"
)

// LoginCredentialsPath returns the path to Klaudia's own stored credentials
// (~/.klaudia/credentials.json, honouring KLAUDIA_CONFIG_DIR). This is what
// `klaudia login` writes and step 3 of ResolveCredential reads.
func LoginCredentialsPath() string {
	return filepath.Join(session.ConfigRoot(), "credentials.json")
}

// loginCredential reads the `klaudia login` store. It shares the Claude Code
// JSON shape (an OAuth session or a stored apiKey), so a future OAuth login can
// write the same file; today `klaudia login` writes only an apiKey. ok is false
// when the file is absent, unreadable, or holds no usable credential.
func loginCredential() (Credential, bool) {
	raw, err := os.ReadFile(LoginCredentialsPath())
	if err != nil {
		return Credential{}, false
	}
	_, cred, ok := parseClaudeCodeCreds(raw)
	return cred, ok
}

// WriteLoginAPIKey stores apiKey to ~/.klaudia/credentials.json with 0600
// permissions (dir 0700), replacing any existing file, and returns the path.
// The resolution chain reads it back at step 3.
func WriteLoginAPIKey(apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", errors.New("empty API key")
	}
	path := LoginCredentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(struct {
		APIKey string `json:"apiKey"`
	}{APIKey: apiKey}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
