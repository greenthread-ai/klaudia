// Package config loads Klaudia settings from .klaudia/config.toml — a project
// .klaudia/ (in the working directory) overlaid on the user's ~/.klaudia/.
// This selects the model provider/endpoint, e.g. an OpenAI-compatible cloud.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Provider names.
const (
	ProviderAnthropic = "anthropic" // default: native Anthropic Messages API
	ProviderOpenAI    = "openai"    // OpenAI-compatible Chat Completions endpoint
)

// Config is the .klaudia/config.toml schema.
type Config struct {
	// Provider selects the backend: "anthropic" (default) or "openai".
	Provider string `toml:"provider,omitempty"`
	// Model is the default model (e.g. "openai/gpt-5.5"); --model overrides it.
	Model string `toml:"model,omitempty"`
	// Theme is the TUI theme for Markdown + chrome (e.g. "nord", "dracula").
	// "" uses the default; /theme overrides it for the current session.
	Theme string `toml:"theme,omitempty"`
	// BaseURL is the OpenAI-compatible endpoint (including /v1), if provider=openai.
	BaseURL string `toml:"baseURL,omitempty"`
	// Temperature for the OpenAI-compatible provider. Omitted from the request
	// when nil (lets the server pick its default).
	Temperature *float64 `toml:"temperature,omitempty"`
	// ContextWindow is the model's effective context size in tokens. Used to
	// drive autocompaction: when the estimated message size approaches this,
	// the loop summarises history to stay below the model's actual cap.
	// 0 (unset) uses the package default of 200000 — fine for Claude, but
	// often wrong for OpenAI-compatible models (e.g. gpt-oss-120b at 128k).
	// Set this when a provider rejects requests with negative max_tokens or
	// "context length exceeded" errors deep into a session.
	ContextWindow int `toml:"contextWindow,omitempty"`
	// MaxTokens is the per-response output-token cap. 0 (unset) uses a
	// model-aware default (api.MaxOutputTokens): capable Claude models get their
	// real limit, unknown/OpenAI-compatible models a safe floor. Set this to
	// raise or lower the cap explicitly — e.g. an OpenAI-compatible model whose
	// output limit the table doesn't know.
	MaxTokens int `toml:"maxTokens,omitempty"`
	// APIKey is the bearer token. Prefer APIKeyEnv to keep secrets out of files.
	APIKey string `toml:"apiKey,omitempty"`
	// APIKeyEnv names an environment variable holding the key.
	APIKeyEnv string `toml:"apiKeyEnv,omitempty"`
	// Sandbox configures how the Bash tool executes commands.
	Sandbox Sandbox `toml:"sandbox,omitempty"`
	// Browser configures local browser-backed tools (BrowserSearch/BrowserFetch and browser tools).
	Browser Browser `toml:"browser,omitempty"`
	// LSP configures language-server code-intelligence tools.
	LSP LSP `toml:"lsp,omitempty"`
	// Permissions holds persisted allow/deny rules for this project.
	Permissions Permissions `toml:"permissions,omitempty"`
	// Trust configures the host-change guardrail.
	Trust Trust `toml:"trust,omitempty"`
}

// Permissions persists allow/deny rule strings (e.g. "Bash(git status:*)",
// "Edit") loaded into the permission context at startup.
// Trust configures the host-change guardrail: how Klaudia behaves when a tool
// call would change the machine it is running on.
//
// This reads command lines and tool inputs. It is a guardrail against
// well-intentioned mistakes, not a security boundary — see [Sandbox] for the
// setting that is actually enforced by the kernel.
type Trust struct {
	// Mode is "enforce" (default), "observe" (classify and report, change no
	// decisions) or "off". Unset means enforce, except for a config that
	// already has permission rules, which starts in observe.
	Mode string `toml:"mode,omitempty"`
}

type Permissions struct {
	// Mode is the default permission mode when no --permission-mode flag is
	// given: default | acceptEdits | bypassPermissions | plan | dontAsk.
	Mode  string   `toml:"mode,omitempty"`
	Allow []string `toml:"allow,omitempty"`
	Deny  []string `toml:"deny,omitempty"`
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
	Mode string `toml:"mode,omitempty"`
	// WriteRoots are extra directories writable under "os" mode (cwd + temp are
	// always writable).
	WriteRoots []string `toml:"writeRoots,omitempty"`
	// Runtime is "docker" (default) or "podman" for container mode.
	Runtime string `toml:"runtime,omitempty"`
	// Image is the container image to run, e.g. "python:3.12-slim".
	Image string `toml:"image,omitempty"`
	// MountCWD bind-mounts the working directory into the container (default true).
	MountCWD *bool `toml:"mountCwd,omitempty"`
	// ReadOnly mounts the working directory read-only.
	ReadOnly bool `toml:"readOnly,omitempty"`
	// Network is the container's --network value (e.g. "none" to isolate).
	Network string `toml:"network,omitempty"`
}

// Browser engines.
const (
	BrowserEngineChrome = "chrome"
)

// Browser configures local browser-backed tools.
type Browser struct {
	// Engine selects the browser engine. Currently only "chrome" is supported.
	Engine string `toml:"engine,omitempty"`
	// Headless controls Chrome headless mode. Defaults to true when unset.
	Headless *bool `toml:"headless,omitempty"`
	// ChromePath overrides the Chrome executable path for launch mode.
	ChromePath string `toml:"chromePath,omitempty"`
	// RemoteURL attaches to an existing Chrome DevTools endpoint instead of launching.
	RemoteURL string `toml:"remoteUrl,omitempty"`
	// UserDataDir stores Chrome profile state/cookies. Defaults to ~/.klaudia/browser/chrome-profile.
	UserDataDir string `toml:"userDataDir,omitempty"`
	// HeadedFallback relaunches headed Chrome for user-assisted search challenge handling.
	HeadedFallback *bool `toml:"headedFallback,omitempty"`
	// SearchEngine selects the default BrowserSearch engine: "ddg" or "google".
	SearchEngine string `toml:"searchEngine,omitempty"`
}

// LSP configures the language-server code-intelligence tools.
type LSP struct {
	// Disabled lists languages to skip even if a server is installed
	// (e.g. ["python", "typescript"]).
	Disabled []string `toml:"disabled,omitempty"`
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

// ProjectPath returns cwd/.klaudia/config.toml.
func ProjectPath(cwd string) string {
	return filepath.Join(ProjectDir(cwd), "config.toml")
}

// ProjectDirExists reports whether cwd/.klaudia exists and is a directory.
func ProjectDirExists(cwd string) bool {
	st, err := os.Stat(ProjectDir(cwd))
	return err == nil && st.IsDir()
}

// AppendProjectPermission appends rule to cwd/.klaudia/config.toml under
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
	data, err := toml.Marshal(cfg)
	if err != nil {
		return false, err
	}
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

// Load reads ~/.klaudia/config.toml then overlays ./.klaudia/config.toml
// (project settings win). Missing files are ignored.
func Load(cwd string) Config {
	var cfg Config
	if home, err := os.UserHomeDir(); err == nil {
		merge(&cfg, read(filepath.Join(home, ".klaudia", "config.toml")))
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
	if err := toml.Unmarshal(data, &c); err != nil {
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
	if src.Theme != "" {
		dst.Theme = src.Theme
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
	if src.Temperature != nil {
		dst.Temperature = src.Temperature
	}
	if src.ContextWindow != 0 {
		dst.ContextWindow = src.ContextWindow
	}
	if src.MaxTokens != 0 {
		dst.MaxTokens = src.MaxTokens
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
	// Permission default mode: project overrides home.
	if src.Permissions.Mode != "" {
		dst.Permissions.Mode = src.Permissions.Mode
	}
	// Permission rules accumulate (home rules + project rules).
	dst.Permissions.Allow = append(dst.Permissions.Allow, src.Permissions.Allow...)
	dst.Permissions.Deny = append(dst.Permissions.Deny, src.Permissions.Deny...)
	if src.Trust.Mode != "" {
		dst.Trust.Mode = src.Trust.Mode
	}
	// Disabled LSP languages accumulate (union of home + project).
	dst.LSP.Disabled = append(dst.LSP.Disabled, src.LSP.Disabled...)
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
