// Package config loads Klaudia settings from .klaudia/config.json — a project
// .klaudia/ (in the working directory) overlaid on the user's ~/.klaudia/.
// This selects the model provider/endpoint, e.g. an OpenAI-compatible cloud.
package config

import (
	"encoding/json"
	"errors"
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
	// Browser configures local browser-backed tools (WebSearch/WebFetch and browser tools).
	Browser Browser `json:"browser,omitempty"`
	// Permissions holds persisted allow/deny rules for this project.
	Permissions Permissions `json:"permissions,omitempty"`
}

// Permissions persists allow/deny rule strings (e.g. "Bash(git status:*)",
// "Edit") loaded into the permission context at startup.
type Permissions struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// Sandbox modes.
const (
	SandboxLocal     = "local"     // unconfined host process (default)
	SandboxOS        = "os"        // OS confinement: sandbox-exec (macOS) / bwrap (Linux)
	SandboxContainer = "container" // run inside a docker/podman container
)

// Sandbox configures Bash command execution.
type Sandbox struct {
	// Mode is "local" (default), "os" (sandbox-exec/bwrap), or "container".
	Mode string `json:"mode,omitempty"`
	// WriteRoots are extra directories writable under "os" mode (cwd + temp are
	// always writable).
	WriteRoots []string `json:"writeRoots,omitempty"`
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

// Browser engines.
const (
	BrowserEngineChrome = "chrome"
)

// Browser configures local browser-backed tools.
type Browser struct {
	// Engine selects the browser engine. Currently only "chrome" is supported.
	Engine string `json:"engine,omitempty"`
	// Headless controls Chrome headless mode. Defaults to true when unset.
	Headless *bool `json:"headless,omitempty"`
	// ChromePath overrides the Chrome executable path for launch mode.
	ChromePath string `json:"chromePath,omitempty"`
	// RemoteURL attaches to an existing Chrome DevTools endpoint instead of launching.
	RemoteURL string `json:"remoteUrl,omitempty"`
	// UserDataDir stores Chrome profile state/cookies. Defaults to ~/.klaudia/browser/chrome-profile.
	UserDataDir string `json:"userDataDir,omitempty"`
	// HeadedFallback relaunches headed Chrome for user-assisted search challenge handling.
	HeadedFallback *bool `json:"headedFallback,omitempty"`
	// SearchEngine selects the default WebSearch engine: "ddg" or "google".
	SearchEngine string `json:"searchEngine,omitempty"`
}

// MountCWDOr returns MountCWD or the default when unset.
func (s Sandbox) MountCWDOr(def bool) bool {
	if s.MountCWD == nil {
		return def
	}
	return *s.MountCWD
}

// ProjectDir returns cwd/.klaudia.
func ProjectDir(cwd string) string {
	return filepath.Join(cwd, ".klaudia")
}

// ProjectPath returns cwd/.klaudia/config.json.
func ProjectPath(cwd string) string {
	return filepath.Join(ProjectDir(cwd), "config.json")
}

// ProjectDirExists reports whether cwd/.klaudia exists and is a directory.
func ProjectDirExists(cwd string) bool {
	st, err := os.Stat(ProjectDir(cwd))
	return err == nil && st.IsDir()
}

// AppendProjectPermission appends rule to cwd/.klaudia/config.json under
// permissions.allow or permissions.deny. It is a no-op when cwd/.klaudia does
// not exist, so users opt into project-local persistence by creating the folder.
func AppendProjectPermission(cwd, kind, rule string) (bool, error) {
	if !ProjectDirExists(cwd) {
		return false, nil
	}
	path := ProjectPath(cwd)
	cfg, err := readProject(path)
	if err != nil {
		return false, err
	}
	switch kind {
	case "allow":
		if contains(cfg.Permissions.Allow, rule) {
			return true, nil
		}
		cfg.Permissions.Allow = append(cfg.Permissions.Allow, rule)
	case "deny":
		if contains(cfg.Permissions.Deny, rule) {
			return true, nil
		}
		cfg.Permissions.Deny = append(cfg.Permissions.Deny, rule)
	default:
		return false, errors.New("permission kind must be allow or deny")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	return true, os.WriteFile(path, data, 0o644)
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// Load reads ~/.klaudia/config.json then overlays ./.klaudia/config.json
// (project settings win). Missing files are ignored.
func Load(cwd string) Config {
	var cfg Config
	if home, err := os.UserHomeDir(); err == nil {
		merge(&cfg, read(filepath.Join(home, ".klaudia", "config.json")))
	}
	merge(&cfg, read(ProjectPath(cwd)))
	return cfg
}

func read(path string) Config {
	c, _ := readProject(path)
	return c
}

func readProject(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return c, nil
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	return c, nil
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
	if len(src.Sandbox.WriteRoots) > 0 {
		dst.Sandbox.WriteRoots = append(dst.Sandbox.WriteRoots, src.Sandbox.WriteRoots...)
	}
	if src.Browser.Engine != "" {
		dst.Browser.Engine = src.Browser.Engine
	}
	if src.Browser.Headless != nil {
		dst.Browser.Headless = src.Browser.Headless
	}
	if src.Browser.ChromePath != "" {
		dst.Browser.ChromePath = src.Browser.ChromePath
	}
	if src.Browser.RemoteURL != "" {
		dst.Browser.RemoteURL = src.Browser.RemoteURL
	}
	if src.Browser.UserDataDir != "" {
		dst.Browser.UserDataDir = src.Browser.UserDataDir
	}
	if src.Browser.HeadedFallback != nil {
		dst.Browser.HeadedFallback = src.Browser.HeadedFallback
	}
	if src.Browser.SearchEngine != "" {
		dst.Browser.SearchEngine = src.Browser.SearchEngine
	}
	// Permission rules accumulate (home rules + project rules).
	dst.Permissions.Allow = append(dst.Permissions.Allow, src.Permissions.Allow...)
	dst.Permissions.Deny = append(dst.Permissions.Deny, src.Permissions.Deny...)
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
