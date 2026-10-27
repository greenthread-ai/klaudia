// Package config loads Klaudia settings from .klaudia/config.toml — a project
// .klaudia/ (in the working directory) overlaid on the user's ~/.klaudia/.
// This selects the model provider/endpoint, e.g. an OpenAI-compatible cloud.
package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/greenthread-ai/klaudia/internal/session"
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
	// BaseURL is the API endpoint, in whichever dialect Provider selects: an
	// OpenAI-compatible base (including the /v1 suffix) under
	// provider="openai", an Anthropic-compatible one (a proxy or gateway)
	// otherwise. Empty uses the provider's own default, which for Anthropic is
	// production and for OpenAI-compatible does not exist — there it is
	// required.
	//
	// Endpoint and dialect are deliberately two keys rather than one: a bare
	// baseURL cannot say whether the host behind it speaks Anthropic or
	// OpenAI, and guessing turns a wrong guess into a protocol-mismatch 400
	// from the server instead of something that names the cause.
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
	// MaxRetries is how many times a transient failure (connection error,
	// 429, 5xx) is retried with backoff. Unset uses 5; 0 disables retrying.
	MaxRetries *int `toml:"maxRetries,omitempty"`
	// StreamIdleTimeout is the seconds a streamed reply may go without data
	// before it is treated as stalled. Unset uses 120; 0 disables the
	// watchdog. Raise it behind a proxy that buffers the stream.
	StreamIdleTimeout *int `toml:"streamIdleTimeout,omitempty"`
	// Effort is how hard the model works per response: "low", "medium",
	// "high", "xhigh" or "max". Unset sends nothing and lets the provider
	// apply its own default (high on most Claude models). --effort and
	// KLAUDIA_EFFORT override it; /effort overrides those for one session.
	Effort string `toml:"effort,omitempty"`
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
	// Permissions holds the session's starting stance for this project.
	Permissions Permissions `toml:"permissions,omitempty"`
	// Input configures the prompt's key handling.
	Input Input `toml:"input,omitempty"`
	// Links configures clickable (OSC 8) links in the TUI.
	Links Links `toml:"links,omitempty"`
	// Subagents configures how sub-agents run.
	Subagents Subagents `toml:"subagents,omitempty"`
	// Hooks are lifecycle hooks: shell commands run at fixed points in a turn.
	//
	// Note that Load does NOT merge this field, and that is deliberate — see
	// the comment on merge. Use LoadHooks, which keeps each hook attached to
	// the file it came from.
	Hooks []Hook `toml:"hooks,omitempty"`
}

// Hook is one `[[hooks]]` entry.
//
// This is the on-disk shape only. internal/hooks compiles it into something
// runnable (the matcher becomes a regexp, the timeout a duration) and owns
// every question about what the fields mean.
type Hook struct {
	// Event names the lifecycle point: SessionStart, UserPromptSubmit,
	// PreToolUse or PostToolUse.
	Event string `toml:"event"`
	// Matcher is a regexp over the tool name, for the two tool events. Empty
	// matches every tool.
	Matcher string `toml:"matcher,omitempty"`
	// Command is the shell command to run, as `sh -c` would take it.
	Command string `toml:"command"`
	// Timeout is a Go duration ("5s", "2m"). Empty uses the package default.
	Timeout string `toml:"timeout,omitempty"`
}

// Subagents configures sub-agent execution.
type Subagents struct {
	// Worktree gives a sub-agent that can write its own git checkout, applied
	// back to the working tree when it finishes. On by default: two children
	// editing one tree both succeed at producing a mess, and the cost of a
	// second checkout is a directory entry plus the uncommitted delta.
	//
	// A pointer so `worktree = false` is distinguishable from the field being
	// absent — with a bool, turning the default off would be impossible.
	//
	// Turn it off for a working tree a second checkout cannot reproduce: one
	// that needs an install step before anything runs, or whose build output
	// is gitignored and expensive. A worktree starts from the parent's tracked
	// and untracked files, never its ignored ones.
	Worktree *bool `toml:"worktree,omitempty"`
	// Effort is the level sub-agents run at: "low", "medium", "high", "xhigh"
	// or "max". Unset means inherit the session's level, which is the default
	// because a quieter sub-agent is a quality trade that should be made
	// deliberately rather than inherited from a default nobody chose.
	Effort string `toml:"effort,omitempty"`
}

// SubagentWorktrees resolves the default: on unless the config says otherwise.
func (c Config) SubagentWorktrees() bool {
	return c.Subagents.Worktree == nil || *c.Subagents.Worktree
}

// Links configures OSC 8 hyperlinks on URLs and file paths in TUI output.
type Links struct {
	// Enabled forces links on or off. Unset detects the terminal; the
	// KLAUDIA_HYPERLINKS environment variable overrides both.
	Enabled *bool `toml:"enabled,omitempty"`
	// File is the URL a file path links to, with {path}, {line} and {col}
	// substituted — e.g. "vscode://file/{path}:{line}:{col}" or
	// "cursor://file/{path}:{line}" to open the editor at the line. Empty
	// links to file://, which the OS opens without a line number.
	File string `toml:"file,omitempty"`
}

// Input configures how the prompt treats the Return key.
type Input struct {
	// Enter is "send" (default) or "newline".
	//
	// "send":    Return submits; ctrl+j and alt+Return insert a newline.
	// "newline": Return inserts a newline; alt+Return and ctrl+j submit.
	//
	// There is deliberately no "ctrl+Return" setting: a terminal cannot tell
	// Ctrl+Return from Return — both send CR — unless it is configured to send
	// something else for that chord. Map it to ESC CR ("\x1b\r") in the
	// terminal and it arrives here as alt+Return.
	Enter string `toml:"enter,omitempty"`
}

// Permissions sets the session's starting stance.
//
// There are no allow/deny rules here any more, and no [trust] section either.
// Per-command rules were the model the host-change guardrail replaced, and the
// guardrail has no posture to configure: it always classifies, and the way to
// turn everything off is the mode that says so.
type Permissions struct {
	// Mode is the starting permission mode when no --permission-mode flag is
	// given: autonomous (default) | plan | bypassPermissions.
	Mode string `toml:"mode,omitempty"`
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

// UserPath returns the user's config file: config.toml under the config root
// (~/.klaudia, or KLAUDIA_CONFIG_DIR). It used to be built from the home
// directory directly, so KLAUDIA_CONFIG_DIR moved sessions, caches and MCP
// config but not the config itself — a sandboxed run silently picked up the
// real user's provider and credentials.
func UserPath() string {
	return filepath.Join(session.ConfigRoot(), "config.toml")
}

// ProjectPath returns cwd/.klaudia/config.toml.
func ProjectPath(cwd string) string {
	return filepath.Join(ProjectDir(cwd), "config.toml")
}

// Load reads ~/.klaudia/config.toml then overlays ./.klaudia/config.toml
// (project settings win). Missing files are ignored.
func Load(cwd string) Config {
	var cfg Config
	merge(&cfg, read(UserPath()))
	merge(&cfg, read(ProjectPath(cwd)))
	return cfg
}

// HookSources is the hook configuration with its provenance intact.
type HookSources struct {
	// User came from ~/.klaudia/config.toml: the user's own machine, written by
	// the person running Klaudia.
	User []Hook
	// Project came from ./.klaudia/config.toml, which arrives with the repo. A
	// clone can carry hooks its author chose, so these are not the same kind of
	// instruction as the ones above and callers must not treat them as such.
	Project []Hook
	// ProjectPath is the file Project was read from, for a prompt that can name
	// what it is asking about.
	ProjectPath string
}

// LoadHooks reads the hook entries from both config files without merging them.
//
// Everything else in Config is a setting, and the only question about a setting
// is which value wins. A hook is a command that will be executed, so where it
// came from is part of what it is: the trust decision in internal/hooks needs
// the project's hooks separable from the user's, and a merged slice cannot
// answer that.
func LoadHooks(cwd string) HookSources {
	src := HookSources{ProjectPath: ProjectPath(cwd)}
	src.User = read(UserPath()).Hooks
	src.Project = read(src.ProjectPath).Hooks
	return src
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
//
// Hooks are left out on purpose. Merging them would produce one slice in which
// a command from the repo is indistinguishable from a command the user wrote,
// and the only safe thing to do with such a slice is refuse to run any of it.
// LoadHooks is the accessor.
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
	if src.MaxRetries != nil {
		dst.MaxRetries = src.MaxRetries
	}
	if src.StreamIdleTimeout != nil {
		dst.StreamIdleTimeout = src.StreamIdleTimeout
	}
	if src.Effort != "" {
		dst.Effort = src.Effort
	}
	if src.Links.Enabled != nil {
		dst.Links.Enabled = src.Links.Enabled
	}
	if src.Links.File != "" {
		dst.Links.File = src.Links.File
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
	if src.Subagents.Worktree != nil {
		dst.Subagents.Worktree = src.Subagents.Worktree
	}
	if src.Subagents.Effort != "" {
		dst.Subagents.Effort = src.Subagents.Effort
	}
	// Disabled LSP languages accumulate (union of home + project).
	dst.LSP.Disabled = append(dst.LSP.Disabled, src.LSP.Disabled...)
}

// ResolveBaseURL returns the configured endpoint, trimmed. "" means the
// provider's own default.
//
// It applies to whichever dialect Provider selects, which baseURL did not do
// before: the key was read only under provider="openai", so an Anthropic proxy
// could be set through KLAUDIA_CUSTOM_ENDPOINT and nowhere else, and a baseURL
// left in the file under the default provider was silently ignored.
func (c Config) ResolveBaseURL() string {
	return strings.TrimSpace(c.BaseURL)
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
