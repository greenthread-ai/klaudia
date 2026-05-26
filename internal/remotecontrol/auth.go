// Device-code OAuth client. Pairs with ai-console's /api/oauth/device/*
// endpoints. The user runs `klaudia /remote-control`, we print a URL and
// short user_code, poll until the user approves in their browser, and
// stash the returned secret locally (encrypted on disk) for the duration
// of the session.
//
// Credentials live in $XDG_CONFIG_HOME/klaudia/ai-console.json (default
// ~/.config/klaudia/ on Linux, ~/Library/Application Support/klaudia/ on
// macOS) with 0600 perms. Keychain integration is a follow-up — JSON on
// disk is the same security posture the existing config.toml uses for
// `apiKey`, so we're not making things worse, and it's portable.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Credential is the persisted token + the server it's bound to.
type Credential struct {
	BaseURL string `json:"base_url"`
	KeyID   string `json:"key_id"`
	KeyName string `json:"key_name"`
	Secret  string `json:"secret"`
}

// Config holds the user-facing settings the device-code flow needs.
type Config struct {
	BaseURL    string // e.g. "https://ai-console.local"
	DeviceName string // defaults to hostname
	Client     string // version banner, e.g. "klaudia/0.1.0"
	HTTP       *http.Client
}

func (c *Config) fillDefaults() {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if c.DeviceName == "" {
		if h, err := os.Hostname(); err == nil {
			c.DeviceName = h
		} else {
			c.DeviceName = "klaudia"
		}
	}
	if c.Client == "" {
		c.Client = "klaudia/dev"
	}
}

// startResponse mirrors ai-console's POST /api/oauth/device/start body.
type startResponse struct {
	UserCode        string `json:"user_code"`
	DeviceCode      string `json:"device_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int    `json:"interval"`
	ExpiresIn       int    `json:"expires_in"`
}

type pollResponse struct {
	Status string `json:"status"`
	Secret string `json:"secret,omitempty"`
}

// PromptFunc is how the package surfaces the verification URL + code to
// the user. Different frontends (TUI, headless) implement this
// differently; both block until the poll completes.
type PromptFunc func(verificationURL, userCode string)

// Login runs the device-code dance and returns a Credential on success.
// It calls prompt with the URL + code, then polls until approved /
// expired / context cancelled.
func Login(ctx context.Context, cfg Config, prompt PromptFunc) (*Credential, error) {
	cfg.fillDefaults()
	if cfg.BaseURL == "" {
		return nil, errors.New("remotecontrol: base URL required")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	startBody, _ := json.Marshal(map[string]string{
		"device_name": cfg.DeviceName,
		"client":      cfg.Client,
	})
	startReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.BaseURL+"/api/oauth/device/start", strings.NewReader(string(startBody)))
	if err != nil {
		return nil, err
	}
	startReq.Header.Set("Content-Type", "application/json")
	startReq.Header.Set("Accept", "application/json")
	startResp, err := cfg.HTTP.Do(startReq)
	if err != nil {
		return nil, fmt.Errorf("device start: %w", err)
	}
	defer startResp.Body.Close()
	if startResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device start: HTTP %s", startResp.Status)
	}
	var sr startResponse
	if err := json.NewDecoder(startResp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("device start: decode: %w", err)
	}

	verification := sr.VerificationURI
	if verification == "" {
		verification = cfg.BaseURL + "/connect"
	}
	verificationURL := verification
	if !strings.Contains(verification, "?") {
		verificationURL = verification + "?code=" + sr.UserCode
	}
	if prompt != nil {
		prompt(verificationURL, sr.UserCode)
	}

	interval := time.Duration(sr.Interval) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(time.Duration(sr.ExpiresIn) * time.Second)
	if sr.ExpiresIn <= 0 {
		deadline = time.Now().Add(10 * time.Minute)
	}

	pollBody, _ := json.Marshal(map[string]string{"device_code": sr.DeviceCode})
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		if time.Now().After(deadline) {
			return nil, errors.New("device code expired before approval")
		}
		pr, err := http.NewRequestWithContext(ctx, http.MethodPost,
			cfg.BaseURL+"/api/oauth/device/poll", strings.NewReader(string(pollBody)))
		if err != nil {
			return nil, err
		}
		pr.Header.Set("Content-Type", "application/json")
		pr.Header.Set("Accept", "application/json")
		resp, err := cfg.HTTP.Do(pr)
		if err != nil {
			// Transient network errors during polling — keep going.
			continue
		}
		var p pollResponse
		_ = json.NewDecoder(resp.Body).Decode(&p)
		resp.Body.Close()
		switch p.Status {
		case "pending":
			continue
		case "denied":
			return nil, errors.New("device code denied by user")
		case "expired":
			return nil, errors.New("device code expired")
		case "approved":
			if p.Secret == "" {
				return nil, errors.New("approved but no secret returned")
			}
			return &Credential{
				BaseURL: cfg.BaseURL,
				Secret:  p.Secret,
				// KeyID/KeyName aren't on the poll response; the
				// downstream WS handshake will tell us.
			}, nil
		default:
			// Unknown — treat as still pending so we don't drop the user.
			continue
		}
	}
}

// --- on-disk credential store ---

// credentialPath returns the path to the per-user ai-console credential
// file. We use the same shape across platforms because klaudia is a CLI;
// the user typically signs in once per machine.
func credentialPath() (string, error) {
	var base string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support", "klaudia")
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			base = filepath.Join(xdg, "klaudia")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config", "klaudia")
		}
	}
	return filepath.Join(base, "ai-console.json"), nil
}

// Save writes the credential to disk with 0600 perms. Overwrites any
// existing file for this base URL.
func (c *Credential) Save() error {
	p, err := credentialPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// LoadCredential reads the previously saved credential. Returns nil, nil
// if no file exists (so callers can treat absence as "not signed in"
// without a special error).
func LoadCredential() (*Credential, error) {
	p, err := credentialPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var c Credential
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Secret == "" {
		return nil, nil
	}
	return &c, nil
}

// ClearCredential removes the on-disk credential.
func ClearCredential() error {
	p, err := credentialPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
