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

func TestLoadBrowserProjectOverridesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, `{"browser":{"engine":"chrome","headless":false,"chromePath":"/home/chrome","remoteUrl":"http://home:9222","userDataDir":"/home/profile","headedFallback":false,"searchEngine":"google"}}`)

	cwd := t.TempDir()
	writeConfig(t, cwd, `{"browser":{"headless":true,"chromePath":"/project/chrome","userDataDir":"/project/profile","headedFallback":true,"searchEngine":"ddg"}}`)

	cfg := Load(cwd)
	if cfg.Browser.Engine != "chrome" {
		t.Errorf("browser.engine = %q, want inherited chrome", cfg.Browser.Engine)
	}
	if cfg.Browser.Headless == nil || *cfg.Browser.Headless != true {
		t.Errorf("browser.headless = %v, want project true", cfg.Browser.Headless)
	}
	if cfg.Browser.ChromePath != "/project/chrome" {
		t.Errorf("browser.chromePath = %q", cfg.Browser.ChromePath)
	}
	if cfg.Browser.RemoteURL != "http://home:9222" {
		t.Errorf("browser.remoteUrl = %q, want inherited home value", cfg.Browser.RemoteURL)
	}
	if cfg.Browser.UserDataDir != "/project/profile" {
		t.Errorf("browser.userDataDir = %q", cfg.Browser.UserDataDir)
	}
	if cfg.Browser.HeadedFallback == nil || *cfg.Browser.HeadedFallback != true {
		t.Errorf("browser.headedFallback = %v, want project true", cfg.Browser.HeadedFallback)
	}
	if cfg.Browser.SearchEngine != "ddg" {
		t.Errorf("browser.searchEngine = %q, want ddg", cfg.Browser.SearchEngine)
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

func TestLoadPermissionsAccumulate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeConfig(t, home, `{"permissions":{"allow":["Edit"],"deny":["Bash(rm:*)"]}}`)
	cwd := t.TempDir()
	writeConfig(t, cwd, `{"permissions":{"allow":["Bash(go test:*)"]}}`)

	cfg := Load(cwd)
	if len(cfg.Permissions.Allow) != 2 {
		t.Errorf("allow = %v, want home+project merged", cfg.Permissions.Allow)
	}
	if len(cfg.Permissions.Deny) != 1 || cfg.Permissions.Deny[0] != "Bash(rm:*)" {
		t.Errorf("deny = %v", cfg.Permissions.Deny)
	}
}

func TestAppendProjectPermission(t *testing.T) {
	cwd := t.TempDir()
	if ok, err := AppendProjectPermission(cwd, "allow", "Edit"); err != nil || ok {
		t.Fatalf("AppendProjectPermission without .klaudia = %v,%v, want false,nil", ok, err)
	}

	if err := os.MkdirAll(filepath.Join(cwd, ".klaudia"), 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, err := AppendProjectPermission(cwd, "allow", "Edit"); err != nil || !ok {
		t.Fatalf("AppendProjectPermission allow = %v,%v, want true,nil", ok, err)
	}
	if ok, err := AppendProjectPermission(cwd, "allow", "Edit"); err != nil || !ok {
		t.Fatalf("AppendProjectPermission duplicate = %v,%v, want true,nil", ok, err)
	}
	if ok, err := AppendProjectPermission(cwd, "deny", "Bash(rm:*)"); err != nil || !ok {
		t.Fatalf("AppendProjectPermission deny = %v,%v, want true,nil", ok, err)
	}

	cfg := Load(cwd)
	if len(cfg.Permissions.Allow) != 1 || cfg.Permissions.Allow[0] != "Edit" {
		t.Errorf("allow = %v, want [Edit]", cfg.Permissions.Allow)
	}
	if len(cfg.Permissions.Deny) != 1 || cfg.Permissions.Deny[0] != "Bash(rm:*)" {
		t.Errorf("deny = %v, want [Bash(rm:*)]", cfg.Permissions.Deny)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := Load(t.TempDir())
	if cfg.Provider != "" {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}
