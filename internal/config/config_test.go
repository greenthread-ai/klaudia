package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	kd := filepath.Join(dir, ".klaudia")
	if err := os.MkdirAll(kd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kd, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectOverridesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, `{"provider":"anthropic","model":"sonnet"}`)

	cwd := t.TempDir()
	writeConfig(t, cwd, `{"provider":"openai","baseURL":"https://x/v1"}`)

	cfg := Load(cwd)
	if cfg.Provider != "openai" {
		t.Errorf("provider = %q, want openai (project wins)", cfg.Provider)
	}
	if cfg.BaseURL != "https://x/v1" {
		t.Errorf("baseURL = %q", cfg.BaseURL)
	}
	// model not set in project → inherited from home.
	if cfg.Model != "sonnet" {
		t.Errorf("model = %q, want sonnet (inherited)", cfg.Model)
	}
}

func TestResolveAPIKey(t *testing.T) {
	if (Config{APIKey: "inline"}).ResolveAPIKey() != "inline" {
		t.Error("inline apiKey not returned")
	}
	t.Setenv("KLAUDIA_TEST_KEY", "from-env")
	if (Config{APIKeyEnv: "KLAUDIA_TEST_KEY"}).ResolveAPIKey() != "from-env" {
		t.Error("apiKeyEnv not resolved from environment")
	}
	if (Config{}).ResolveAPIKey() != "" {
		t.Error("empty config should yield empty key")
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Load(t.TempDir())
	if cfg.Provider != "" {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}
