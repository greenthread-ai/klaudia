// Package config loads Klaudia settings from .klaudia/config.json — a project
// .klaudia/ (in the working directory) overlaid on the user's ~/.klaudia/.
// This selects the model provider/endpoint, e.g. an OpenAI-compatible cloud.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Provider names.
const (
	ProviderAnthropic = "anthropic" // default: native Anthropic Messages API
	ProviderOpenAI    = "openai"    // OpenAI-compatible Chat Completions endpoint
)

// Config is the .klaudia/config.json schema.
type Config struct {
	// Provider selects the backend: "anthropic" (default) or "openai".
	Provider string `json:"provider,omitempty"`
	// Model is the default model (e.g. "openai/gpt-5.5"); --model overrides it.
	Model string `json:"model,omitempty"`
	// BaseURL is the OpenAI-compatible endpoint (including /v1), if provider=openai.
	BaseURL string `json:"baseURL,omitempty"`
	// APIKey is the bearer token. Prefer APIKeyEnv to keep secrets out of files.
	APIKey string `json:"apiKey,omitempty"`
	// APIKeyEnv names an environment variable holding the key.
	APIKeyEnv string `json:"apiKeyEnv,omitempty"`
	// Sandbox configures how the Bash tool executes commands.
	Sandbox Sandbox `json:"sandbox,omitempty"`
}

// Sandbox modes.
const (
	SandboxLocal     = "local"     // unconfined host process (default)
	SandboxContainer = "container" // run inside a docker/podman container
)

// Sandbox configures Bash command execution.
type Sandbox struct {
	// Mode is "local" (default) or "container".
	Mode string `json:"mode,omitempty"`
	// Runtime is "docker" (default) or "podman" for container mode.
	Runtime string `json:"runtime,omitempty"`
	// Image is the container image to run, e.g. "python:3.12-slim".
	Image string `json:"image,omitempty"`
	// MountCWD bind-mounts the working directory into the container (default true).
	MountCWD *bool `json:"mountCwd,omitempty"`
	// ReadOnly mounts the working directory read-only.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Network is the container's --network value (e.g. "none" to isolate).
	Network string `json:"network,omitempty"`
}

// MountCWDOr returns MountCWD or the default when unset.
func (s Sandbox) MountCWDOr(def bool) bool {
	if s.MountCWD == nil {
		return def
	}
	return *s.MountCWD
}

// Load reads ~/.klaudia/config.json then overlays ./.klaudia/config.json
// (project settings win). Missing files are ignored.
func Load(cwd string) Config {
	var cfg Config
	if home, err := os.UserHomeDir(); err == nil {
		merge(&cfg, read(filepath.Join(home, ".klaudia", "config.json")))
	}
	merge(&cfg, read(filepath.Join(cwd, ".klaudia", "config.json")))
	return cfg
}

func read(path string) Config {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

// merge overlays non-empty fields of src onto dst.
func merge(dst *Config, src Config) {
	if src.Provider != "" {
		dst.Provider = src.Provider
	}
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if src.APIKeyEnv != "" {
		dst.APIKeyEnv = src.APIKeyEnv
	}
	if src.Sandbox.Mode != "" {
		dst.Sandbox.Mode = src.Sandbox.Mode
	}
	if src.Sandbox.Runtime != "" {
		dst.Sandbox.Runtime = src.Sandbox.Runtime
	}
	if src.Sandbox.Image != "" {
		dst.Sandbox.Image = src.Sandbox.Image
	}
	if src.Sandbox.MountCWD != nil {
		dst.Sandbox.MountCWD = src.Sandbox.MountCWD
	}
	if src.Sandbox.ReadOnly {
		dst.Sandbox.ReadOnly = true
	}
	if src.Sandbox.Network != "" {
		dst.Sandbox.Network = src.Sandbox.Network
	}
}

// ResolveAPIKey returns the inline key, or the value of the named env var.
func (c Config) ResolveAPIKey() string {
	if c.APIKey != "" {
		return c.APIKey
	}
	if c.APIKeyEnv != "" {
		return strings.TrimSpace(os.Getenv(c.APIKeyEnv))
	}
	return ""
}
