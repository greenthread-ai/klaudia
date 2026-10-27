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
	if err := os.WriteFile(filepath.Join(kd, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadProjectOverridesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KLAUDIA_CONFIG_DIR", "") // so HOME decides the config root
	writeConfig(t, home, `provider = "anthropic"
model = "sonnet"
`)

	cwd := t.TempDir()
	writeConfig(t, cwd, `provider = "openai"
baseURL = "https://x/v1"
`)

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

func TestLoadThemeProjectOverridesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KLAUDIA_CONFIG_DIR", "") // so HOME decides the config root
	writeConfig(t, home, `theme = "nord"`)

	// Global-only: inherited.
	if cfg := Load(t.TempDir()); cfg.Theme != "nord" {
		t.Errorf("theme = %q, want nord (from home)", cfg.Theme)
	}

	// Project overrides global.
	cwd := t.TempDir()
	writeConfig(t, cwd, `theme = "dracula"`)
	if cfg := Load(cwd); cfg.Theme != "dracula" {
		t.Errorf("theme = %q, want dracula (project wins)", cfg.Theme)
	}
}

func TestLoadPermissionModeOverridesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KLAUDIA_CONFIG_DIR", "") // so HOME decides the config root
	writeConfig(t, home, "[permissions]\nmode = \"plan\"\n")

	// Global-only → inherited.
	if cfg := Load(t.TempDir()); cfg.Permissions.Mode != "plan" {
		t.Errorf("mode = %q, want plan (from home)", cfg.Permissions.Mode)
	}
	// Project overrides global.
	cwd := t.TempDir()
	writeConfig(t, cwd, "[permissions]\nmode = \"plan\"\n")
	if cfg := Load(cwd); cfg.Permissions.Mode != "plan" {
		t.Errorf("mode = %q, want plan (project wins)", cfg.Permissions.Mode)
	}
}

func TestLoadBrowserProjectOverridesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KLAUDIA_CONFIG_DIR", "") // so HOME decides the config root
	writeConfig(t, home, `
[browser]
engine = "chrome"
headless = false
chromePath = "/home/chrome"
remoteUrl = "http://home:9222"
userDataDir = "/home/profile"
headedFallback = false
searchEngine = "google"
`)

	cwd := t.TempDir()
	writeConfig(t, cwd, `
[browser]
headless = true
chromePath = "/project/chrome"
userDataDir = "/project/profile"
headedFallback = true
searchEngine = "ddg"
`)

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
func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KLAUDIA_CONFIG_DIR", "") // so HOME decides the config root
	cfg := Load(t.TempDir())
	if cfg.Provider != "" {
		t.Errorf("expected empty config, got %+v", cfg)
	}
}

// The default is on, so the only interesting case is turning it off: with a
// plain bool, `worktree = false` is indistinguishable from the section being
// absent and the setting would have no way to work at all.
func TestSubagentWorktrees(t *testing.T) {
	tests := []struct {
		name string
		home string
		proj string
		want bool
	}{
		{name: "no config anywhere", want: true},
		{name: "off in the user's config", home: "[subagents]\nworktree = false\n", want: false},
		{name: "off in the project", proj: "[subagents]\nworktree = false\n", want: false},
		{
			name: "the project turns it back on",
			home: "[subagents]\nworktree = false\n",
			proj: "[subagents]\nworktree = true\n",
			want: true,
		},
		{
			// An unrelated project section must not read as "unset" and lose
			// the user's choice.
			name: "a project section that says nothing about it inherits",
			home: "[subagents]\nworktree = false\n",
			proj: "model = \"sonnet\"\n",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("KLAUDIA_CONFIG_DIR", "") // so HOME decides the config root
			if tt.home != "" {
				writeConfig(t, home, tt.home)
			}
			cwd := t.TempDir()
			if tt.proj != "" {
				writeConfig(t, cwd, tt.proj)
			}
			if got := Load(cwd).SubagentWorktrees(); got != tt.want {
				t.Errorf("SubagentWorktrees = %v, want %v", got, tt.want)
			}
		})
	}
}

// KLAUDIA_CONFIG_DIR moves the user's config file along with everything else
// under the config root. It used to move sessions and caches but not the
// config, so a run pointed at a scratch root still read the real user's
// provider and credentials.
func TestLoadHonoursConfigDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", root)
	t.Setenv("HOME", t.TempDir()) // a config here must not be the one read
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("provider = \"openai\"\nmodel = \"from-config-dir\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load(t.TempDir()).Model; got != "from-config-dir" {
		t.Errorf("Load().Model = %q, want the config under KLAUDIA_CONFIG_DIR", got)
	}
	if got := UserPath(); got != filepath.Join(root, "config.toml") {
		t.Errorf("UserPath() = %q", got)
	}
}
