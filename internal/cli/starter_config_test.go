package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/permission"
)

// clearProviderEnv isolates the test from the developer's credentials: every
// variable buildProvider or the starter's examples read starts empty.
func clearProviderEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, name := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "KLAUDIA_CUSTOM_ENDPOINT", "MY_API_KEY"} {
		t.Setenv(name, "")
	}
}

// loadStarter decodes the file at path strictly — a key the schema does not
// know is an error — and then loads it the way run() does.
func loadStarter(t *testing.T, path, cwd string) config.Config {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := toml.NewDecoder(f)
	dec.DisallowUnknownFields()
	var strict config.Config
	if err := dec.Decode(&strict); err != nil {
		t.Fatalf("starter does not decode strictly: %v", err)
	}
	return config.Load(cwd)
}

// validateStarter runs the checks run() applies to a loaded config before a
// session starts, and fails the test on the first one that would stop it.
func validateStarter(t *testing.T, cfg config.Config) api.Provider {
	t.Helper()
	provider, _, err := buildProvider(cfg)
	if err != nil {
		t.Fatalf("buildProvider: %v", err)
	}
	if _, err := permission.ParseRules(cfg.Permissions.Allow); err != nil {
		t.Errorf("permissions.allow: %v", err)
	}
	if _, err := permission.ParseRules(cfg.Permissions.Deny); err != nil {
		t.Errorf("permissions.deny: %v", err)
	}
	if m := cfg.Permissions.Mode; m != "" && !permission.Mode(m).Valid() {
		t.Errorf("permissions.mode %q is not valid", m)
	}
	themeOrWarn(cfg.Theme, func(m string) { t.Errorf("theme: %s", m) })
	return provider
}

// The issue this guards (#123): a user with only ANTHROPIC_API_KEY ran
// --create-config, and the next start failed with `provider "openai" needs
// apiKey…` because the starter pointed at a placeholder OpenAI endpoint. The
// file as written must start such a user on Anthropic.
func TestStarterConfigLoadsWithOnlyAnthropicKey(t *testing.T) {
	for _, scope := range []string{"global", "local"} {
		t.Run(scope, func(t *testing.T) {
			clearProviderEnv(t)
			t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
			cwd := t.TempDir()
			path, err := createConfig(scope, cwd)
			if err != nil {
				t.Fatal(err)
			}
			cfg := loadStarter(t, path, cwd)
			if cfg.Provider != config.ProviderAnthropic {
				t.Errorf("provider = %q, want %q", cfg.Provider, config.ProviderAnthropic)
			}
			if cfg.BaseURL != "" || cfg.APIKeyEnv != "" || cfg.Model != "" {
				t.Errorf("OpenAI-only settings are active: baseURL=%q apiKeyEnv=%q model=%q", cfg.BaseURL, cfg.APIKeyEnv, cfg.Model)
			}
			if _, ok := validateStarter(t, cfg).(*api.Client); !ok {
				t.Error("starter did not select the Anthropic client")
			}
		})
	}
}

// The commented OpenAI-compatible block is an alternative to the provider
// line, not an addition to it, so it is indented inside the comment. Swapped
// in as its guidance says, it must load and select the OpenAI provider.
func TestStarterConfigOpenAIBlockLoadsWhenSwappedIn(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("MY_API_KEY", "sk-test")
	cwd := t.TempDir()
	path, err := createConfig("local", cwd)
	if err != nil {
		t.Fatal(err)
	}

	var out []string
	inBlock, swapped := false, 0
	for _, line := range strings.Split(starterConfig, "\n") {
		switch {
		case line == `provider = "anthropic"`:
			continue // "replace the provider line above with these lines"
		case line == `#   provider = "openai"`:
			inBlock = true
		}
		if inBlock {
			line = strings.TrimPrefix(line, "#")
			swapped++
			if strings.TrimSpace(line) == `apiKeyEnv = "MY_API_KEY"` {
				inBlock = false
			}
		}
		out = append(out, line)
	}
	if swapped < 4 || inBlock {
		t.Fatalf("OpenAI block not found intact in the starter (swapped %d lines)", swapped)
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := loadStarter(t, path, cwd)
	if cfg.Provider != config.ProviderOpenAI || cfg.BaseURL == "" || cfg.Model == "" || cfg.APIKeyEnv != "MY_API_KEY" {
		t.Fatalf("OpenAI block did not load: provider=%q baseURL=%q model=%q apiKeyEnv=%q",
			cfg.Provider, cfg.BaseURL, cfg.Model, cfg.APIKeyEnv)
	}
	if _, ok := validateStarter(t, cfg).(*api.OpenAIProvider); !ok {
		t.Error("OpenAI block did not select the OpenAI-compatible provider")
	}
}
