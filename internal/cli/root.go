package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/browser"
	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/doctor"
	"github.com/greenthread-ai/klaudia/internal/lsp"
	"github.com/greenthread-ai/klaudia/internal/mcp"
	"github.com/greenthread-ai/klaudia/internal/memory"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/prompt"
	"github.com/greenthread-ai/klaudia/internal/sandbox"
	"github.com/greenthread-ai/klaudia/internal/session"
	"github.com/greenthread-ai/klaudia/internal/skill"
	"github.com/greenthread-ai/klaudia/internal/streamjson"
	"github.com/greenthread-ai/klaudia/internal/subagent"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
	"github.com/greenthread-ai/klaudia/internal/tui"
	"github.com/greenthread-ai/klaudia/internal/version"
)

func compactAndPersist(ctx context.Context, history []anthropic.BetaMessageParam, compact tui.CompactFunc, onSummary func(string)) ([]anthropic.BetaMessageParam, string, error) {
	newHistory, summary, err := compact(ctx, history)
	if err == nil && onSummary != nil {
		onSummary(summary)
	}
	return newHistory, summary, err
}

// withAgentTool returns a registry that is the base tools plus the Agent tool,
// wired to a sub-agent spawner that draws from the base tools.
func withAgentTool(base *tools.Registry, provider api.Provider, model anthropic.Model, perm permission.Context, approver agent.Approver, maxTurns int, deferred map[string]bool, workingDir string, host *agent.HostGate) (*tools.Registry, error) {
	spawner := agent.NewSpawnerWithDeferred(provider, base, model, perm, approver, maxTurns, deferred).
		WithWorkingDir(workingDir).
		WithHostGate(host)

	infos := make([]tools.AgentTypeInfo, 0)
	for _, t := range subagent.Builtin() {
		infos = append(infos, tools.AgentTypeInfo{Name: t.Name, Description: t.Description})
	}
	agentTool, err := tools.NewAgent(spawner, infos)
	if err != nil {
		return nil, err
	}
	return tools.NewRegistry(append(base.All(), agentTool)...), nil
}

// skillToolInfos adapts loaded skills into the tools package's SkillInfo,
// binding each skill's Render so the tools package stays decoupled from skill.
func skillToolInfos(skills []skill.Skill) []tools.SkillInfo {
	infos := make([]tools.SkillInfo, 0, len(skills))
	for _, sk := range skills {
		sk := sk
		infos = append(infos, tools.SkillInfo{
			Name:        sk.Name,
			Description: sk.Description,
			Render:      sk.Render,
		})
	}
	return infos
}

// builtinSlashCommands are the TUI commands a skill cannot override (the slash
// switch handles them before the /<skill> default branch).
var builtinSlashCommands = map[string]bool{
	"help": true, "?": true, "quit": true, "exit": true, "clear": true,
	"model": true, "mode": true, "goal": true,
	"memory": true, "mcp": true, "stats": true,
	"allow": true, "deny": true, "status": true,
	"config": true, "agents": true, "context": true,
	"compact": true, "add-dir": true,
	"plan": true, "doctor": true, "diff": true, "commit": true, "export": true,
	"last": true,
}

// withExtraDirs appends an "additional working directories" note to the system
// prompt when /add-dir has registered any (v1: informational context only).
func withExtraDirs(sys string, dirs []string) string {
	if len(dirs) == 0 {
		return sys
	}
	return sys + "\n\nAdditional working directories the user has made available:\n- " +
		strings.Join(dirs, "\n- ")
}

// buildDoctorInput gathers the facts /doctor reports, without prompting.
func buildDoctorInput(cfg config.Config, model anthropic.Model, cwd string, mcpServers int) doctor.Input {
	servers, hints := lsp.Survey(cwd)
	lspServers := make([]doctor.LSPServer, 0, len(servers))
	for _, s := range servers {
		lspServers = append(lspServers, doctor.LSPServer{Name: s.Bin, Language: s.Language, Version: s.Version})
	}
	ctxLimit, ctxSource := api.ContextWindow(string(model), cfg.ContextWindow)
	in := doctor.Input{
		Provider:        providerName(cfg),
		Model:           string(model),
		SandboxMode:     sandboxMode(cfg.Sandbox),
		ConfigFound:     configFileExists(cwd),
		MCPServers:      mcpServers,
		LSPServers:      lspServers,
		MissingLSPHints: hints,
		AuthKind:        "none",
		ContextWindow:   ctxLimit,
		ContextSource:   ctxSource,
	}
	if cfg.Provider == config.ProviderOpenAI {
		if cfg.ResolveAPIKey() != "" {
			in.AuthOK, in.AuthKind = true, "api-key"
		}
	} else if cred, err := api.ResolveCredential(); err == nil {
		in.AuthOK = true
		if cred.IsOAuth() {
			in.AuthKind = "oauth"
		} else {
			in.AuthKind = "api-key"
		}
	}
	return in
}

// configFileExists reports whether a home or project .klaudia/config.toml exists.
func configFileExists(cwd string) bool {
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := os.Stat(filepath.Join(home, ".klaudia", "config.toml")); err == nil {
			return true
		}
	}
	_, err := os.Stat(config.ProjectPath(cwd))
	return err == nil
}

// providerName returns the resolved provider for display ("anthropic" default).
func providerName(cfg config.Config) string {
	if cfg.Provider != "" {
		return cfg.Provider
	}
	return config.ProviderAnthropic
}

// sandboxMode returns the resolved sandbox mode for display ("local" default).
func sandboxMode(sb config.Sandbox) string {
	if sb.Mode != "" {
		return sb.Mode
	}
	return config.SandboxLocal
}

// themeOrWarn passes a configured theme through, warning (and falling back to
// the default) when it names an unknown theme so a typo isn't silent.
func themeOrWarn(theme string, warn func(string)) string {
	if theme == "" || tui.IsTheme(theme) {
		return theme
	}
	warn(fmt.Sprintf("unknown theme %q in config; using default (valid: %s)", theme, tui.ThemeNames()))
	return ""
}

// tuiAgents adapts the built-in sub-agent types into the TUI's AgentInfo for
// the /agents command.
func tuiAgents() []tui.AgentInfo {
	bs := subagent.Builtin()
	out := make([]tui.AgentInfo, 0, len(bs))
	for _, t := range bs {
		out = append(out, tui.AgentInfo{Name: t.Name, Description: t.Description})
	}
	return out
}

// tuiSkills adapts loaded skills into TUI /<name> commands, warning when a skill
// name shadows a built-in command (the built-in wins, so the skill is
// unreachable as a slash command but remains callable via the Skill tool).
func tuiSkills(skills []skill.Skill, warn func(string)) []tui.SkillCommand {
	out := make([]tui.SkillCommand, 0, len(skills))
	for _, sk := range skills {
		sk := sk
		if builtinSlashCommands[sk.Name] {
			warn(fmt.Sprintf("skill %q shadows built-in /%s; reachable only via the Skill tool", sk.Name, sk.Name))
		}
		out = append(out, tui.SkillCommand{Name: sk.Name, Description: sk.Description, Render: sk.Render})
	}
	return out
}

// modelLister exposes the provider's model enumeration to the TUI when it has
// one. Both shipped providers do, but the capability is an optional interface
// rather than part of Provider — so a future backend that can't list models
// still satisfies Provider, and /model degrades to accepting a typed id.
func modelLister(p api.Provider) func(context.Context) ([]api.ModelInfo, error) {
	lister, ok := p.(api.ModelLister)
	if !ok {
		return nil
	}
	return lister.ListModels
}

// buildProvider selects and constructs the model provider from config. It
// returns the provider and the provider's default model. Anthropic is the
// default; "openai" uses an OpenAI-compatible Chat Completions endpoint.
func buildProvider(cfg config.Config) (api.Provider, string, error) {
	switch cfg.Provider {
	case config.ProviderOpenAI:
		if cfg.BaseURL == "" {
			return nil, "", fmt.Errorf("provider \"openai\" requires baseURL in ~/.klaudia/config.toml or ./.klaudia/config.toml (try --create-config=global or --create-config=local)")
		}
		key := cfg.ResolveAPIKey()
		if key == "" {
			return nil, "", fmt.Errorf("provider \"openai\" needs apiKey or apiKeyEnv in ~/.klaudia/config.toml or ./.klaudia/config.toml; if using apiKeyEnv, export that variable before running")
		}
		return api.NewOpenAIProvider(cfg.BaseURL, key, cfg.Temperature), cfg.Model, nil
	default:
		cred, err := api.ResolveCredential()
		if err != nil {
			return nil, "", err
		}
		return api.New(cred, os.Getenv("KLAUDIA_CUSTOM_ENDPOINT")), cfg.Model, nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func buildBrowserOptions(bc config.Browser) browser.Options {
	opts := browser.Options{
		Mode:           browser.ModeLaunch,
		Headless:       true,
		ChromePath:     strings.TrimSpace(os.Getenv("KLAUDIA_CHROME_PATH")),
		UserDataDir:    firstNonEmpty(strings.TrimSpace(os.Getenv("KLAUDIA_CHROME_USER_DATA_DIR")), browser.DefaultUserDataDir()),
		RemoteURL:      strings.TrimSpace(os.Getenv("KLAUDIA_CHROME_REMOTE_URL")),
		HeadedFallback: true,
	}
	if bc.Headless != nil {
		opts.Headless = *bc.Headless
	} else if v := strings.TrimSpace(strings.ToLower(os.Getenv("KLAUDIA_BROWSER_HEADLESS"))); v != "" {
		switch v {
		case "1", "true", "yes", "on":
			opts.Headless = true
		case "0", "false", "no", "off":
			opts.Headless = false
		}
	}
	if bc.ChromePath != "" {
		opts.ChromePath = bc.ChromePath
	}
	if bc.RemoteURL != "" {
		opts.RemoteURL = bc.RemoteURL
	}
	if bc.UserDataDir != "" {
		opts.UserDataDir = bc.UserDataDir
	}
	if bc.HeadedFallback != nil {
		opts.HeadedFallback = *bc.HeadedFallback
	} else if v := strings.TrimSpace(strings.ToLower(os.Getenv("KLAUDIA_BROWSER_HEADED_FALLBACK"))); v != "" {
		switch v {
		case "1", "true", "yes", "on":
			opts.HeadedFallback = true
		case "0", "false", "no", "off":
			opts.HeadedFallback = false
		}
	}
	if bc.SearchEngine != "" {
		opts.SearchEngine = bc.SearchEngine
	}
	if opts.RemoteURL != "" {
		opts.Mode = browser.ModeAttach
	}
	return opts
}

// buildExecutor selects the Bash execution backend from config. Both "os"
// (host confinement via sandbox-exec/bwrap) and "container" modes degrade
// gracefully to the local executor when the required tool is absent or the
// config is incomplete (warn explains why).
func buildExecutor(sb config.Sandbox, warn func(string)) sandbox.Executor {
	switch sb.Mode {
	case config.SandboxOS:
		return buildOSExecutor(sb, warn)
	case config.SandboxContainer:
		return buildContainerExecutor(sb, warn)
	default:
		return sandbox.NewLocal()
	}
}

// buildOSExecutor picks the OS-native confinement tool for the current
// platform: sandbox-exec on macOS, bubblewrap on Linux. Falls back to local
// (unconfined) execution with a warning when the tool isn't available.
func buildOSExecutor(sb config.Sandbox, warn func(string)) sandbox.Executor {
	switch goruntime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			warn("sandbox mode \"os\": sandbox-exec not found; falling back to local execution")
			return sandbox.NewLocal()
		}
		return sandbox.NewSeatbelt(sb.WriteRoots, sb.Network)
	case "linux":
		if _, err := exec.LookPath("bwrap"); err != nil {
			warn("sandbox mode \"os\": bwrap (bubblewrap) not found; falling back to local execution")
			return sandbox.NewLocal()
		}
		return sandbox.NewBwrap(sb.WriteRoots, sb.Network)
	default:
		warn("sandbox mode \"os\" is unsupported on " + goruntime.GOOS + "; falling back to local execution")
		return sandbox.NewLocal()
	}
}

func buildContainerExecutor(sb config.Sandbox, warn func(string)) sandbox.Executor {
	runtime := sb.Runtime
	if runtime == "" {
		runtime = "docker"
	}
	switch {
	case sb.Image == "":
		warn("sandbox mode \"container\" has no image set; falling back to local execution")
	case !sandbox.RuntimeAvailable(runtime):
		warn(runtime + " is not installed; falling back to local execution")
	default:
		return sandbox.NewContainer(runtime, sb.Image, sb.MountCWDOr(true), sb.ReadOnly, sb.Network)
	}
	return sandbox.NewLocal()
}

// mcpController adapts the mcp.Manager to the TUI's MCPController so /mcp can
// inspect and reconnect/disconnect servers without the TUI owning the manager.
type mcpController struct {
	mgr *mcp.Manager
	ctx context.Context
}

func (c mcpController) Servers() []tui.MCPServerInfo {
	// Count live tools per server (mcp__<server>__<tool>).
	counts := map[string]int{}
	for _, t := range c.mgr.Tools(c.ctx) {
		n := t.Name()
		const pfx = "mcp__"
		if !strings.HasPrefix(n, pfx) {
			continue
		}
		if i := strings.Index(n[len(pfx):], "__"); i >= 0 {
			counts[n[len(pfx):len(pfx)+i]]++
		}
	}
	out := make([]tui.MCPServerInfo, 0)
	for _, s := range c.mgr.Servers() {
		out = append(out, tui.MCPServerInfo{Name: s.Name, Connected: s.Connected(), Tools: counts[s.Name]})
	}
	return out
}

func (c mcpController) Reconnect(name string) error  { return c.mgr.Reconnect(name) }
func (c mcpController) Disconnect(name string) error { return c.mgr.Disconnect(name) }

// gitBranch returns the current git branch for dir, or "" if not a repo.
func gitBranch(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCommit returns the short HEAD commit hash, or "" outside a repo.
func gitCommit(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

const starterConfig = `# Klaudia config
# Global: ~/.klaudia/config.toml
# Local:  ./.klaudia/config.toml (overrides global settings)

provider = "openai"
model = "openai/gpt-5.5"
baseURL = "https://api.example.com/v1"

# apiKeyEnv is the NAME of the environment variable that holds your API key:
# pick any name, then export a variable of that name, e.g. for the value below:
#   export MY_API_KEY="sk-..."
# Or set apiKey = "sk-..." inline — but the env form keeps secrets out of files.
apiKeyEnv = "MY_API_KEY"

# Temperature for the model. Omitted by default (server picks its own default).
# Some providers reject this field; remove the line to omit it from the request.
# temperature = 1.0

# Context window in tokens — drives autocompaction so long sessions don't
# overflow the model upstream ("max_tokens must be at least 1, got -N" or
# "context length exceeded"). Defaults to 200000 (Anthropic-sized); set this
# to the actual window your provider/model exposes. Common values: 8192, 16384,
# 32768, 128000.
# contextWindow = 8192

# TUI theme (Markdown + chrome). /theme switches it for a session.
# theme = "nord" # dracula | gruvbox | tokyo-night | nord | catppuccin

# Optional examples:
# [sandbox]
# mode = "local" # local | os | container
#
# [browser]
# searchEngine = "ddg" # ddg | google
# headless = true
#
# [permissions]
# mode = "autonomous" # autonomous | plan | bypassPermissions
# allow = ["Bash(go test:*)"]
# deny = ["Bash(rm:*)"]
`

func createConfig(scope, cwd string) (string, error) {
	var path string
	switch scope {
	case "global":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		path = filepath.Join(home, ".klaudia", "config.toml")
	case "local":
		path = config.ProjectPath(cwd)
	default:
		return "", fmt.Errorf("--create-config must be global or local")
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("config already exists: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(starterConfig), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// options holds parsed CLI flags, mirroring the JS commander surface
// (08-entry.js setupCommander). Only the Phase 0 subset is wired so far.
type options struct {
	print            bool
	prompt           string
	model            string
	outputFormat     string
	inputFormat      string
	permissionMode   string
	allowHostChanges bool
	dangerouslySkip  bool
	verbose          bool
	maxTurns         int
	resume           string // --resume <session-id>
	continueSession  bool   // --continue
	newSession       bool   // --new-session
	forkSession      bool   // --fork-session
	fullResume       bool   // --full (replay entire transcript, not the summary)

	allowedTools    []string
	disallowedTools []string
	partialMessages bool   // --include-partial-messages
	createConfig    string // --create-config global|local
	loop            bool   // --loop: autonomous goal-spec iteration
	maxIterations   int    // --max-iterations: outer-loop cap for --loop
}

// resolveResumeID selects the prior session to seed from, if any.
// resolveResumeID selects the prior session to seed from, if any. An explicit
// --resume/--continue is honored in any mode; the implicit "pick up the most
// recent project session" is an interactive convenience only, so headless (-p)
// and embedding (stream-json) runs stay stateless unless asked.
func resolveResumeID(cwd string, opts options, interactive bool) (string, error) {
	if opts.newSession && (opts.resume != "" || opts.continueSession) {
		return "", fmt.Errorf("--new-session cannot be combined with --resume or --continue")
	}
	if opts.resume != "" {
		return opts.resume, nil
	}
	if opts.continueSession {
		if id, ok := session.MostRecent(cwd); ok {
			return id, nil
		}
		return "", fmt.Errorf("--continue: no previous session found in this directory")
	}
	if interactive && !opts.newSession {
		if id, ok := session.MostRecent(cwd); ok {
			return id, nil
		}
	}
	return "", nil
}

// NewRootCommand builds the top-level `klaudia` command.
func NewRootCommand() *cobra.Command {
	var opts options

	cmd := &cobra.Command{
		Use:   "klaudia [prompt]",
		Short: "Klaudia — a locally-buildable, extensible agentic coding tool",
		// We render our own version string to match the JS reference exactly.
		Version:       fmt.Sprintf("%s (%s)", version.Version, version.Name),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// A positional prompt is shorthand for -p "<prompt>".
			if opts.prompt == "" && len(args) > 0 {
				opts.prompt = args[0]
				opts.print = true
			}
			return run(cmd, &opts)
		},
	}

	// Match commander's `--version` output: "<version> (Klaudia)" with no prefix.
	cmd.SetVersionTemplate("{{.Version}}\n")

	// A malformed command line is a usage error, not a run failure: nothing
	// happened, and the caller should fix the invocation rather than retry it.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageErrorf("%s", err)
	})
	f := cmd.Flags()
	f.BoolVarP(&opts.print, "print", "p", false, "Non-interactive mode: print result to stdout and exit")
	f.StringVar(&opts.model, "model", "", "Model alias (haiku|sonnet|opus) or full model ID")
	f.StringVar(&opts.outputFormat, "output-format", "text", "Output format: text|json|stream-json")
	f.StringVar(&opts.inputFormat, "input-format", "text", "Input format: text|stream-json (stream-json drives a persistent agent over stdin)")
	f.StringVar(&opts.permissionMode, "permission-mode", "", "Permission mode: autonomous|plan|bypassPermissions (default: config [permissions] mode, else autonomous)")
	f.BoolVar(&opts.allowHostChanges, "allow-host-changes", false, "Non-interactive runs: permit changes to this machine (packages, services, /etc, …) without a human to approve them")
	f.BoolVar(&opts.dangerouslySkip, "dangerously-skip-permissions", false, "Skip all permission checks (sets bypassPermissions)")
	f.BoolVar(&opts.verbose, "verbose", false, "Verbose output (required for stream-json)")
	f.IntVar(&opts.maxTurns, "max-turns", 0, "Limit the number of agentic loop turns (0 = unlimited)")
	f.StringVarP(&opts.resume, "resume", "r", "", "Resume a session by ID")
	f.BoolVar(&opts.continueSession, "continue", false, "Resume the most recent session in this directory (default when available)")
	f.BoolVar(&opts.newSession, "new-session", false, "Start a fresh session instead of auto-resuming the most recent session in this directory")
	f.BoolVar(&opts.forkSession, "fork-session", false, "When resuming, start a new session ID (preserves the original)")
	f.BoolVar(&opts.fullResume, "full", false, "When resuming, replay the entire transcript instead of the compacted summary")
	f.StringSliceVar(&opts.allowedTools, "allowedTools", nil, "Auto-allow tool rules, e.g. 'Edit' or 'Bash(git status:*)' (repeatable, comma-separated)")
	f.StringSliceVar(&opts.disallowedTools, "disallowedTools", nil, "Deny tool rules (same format as --allowedTools)")
	f.BoolVar(&opts.partialMessages, "include-partial-messages", false, "Include partial message chunks as they arrive (only with --print and --output-format=stream-json)")
	f.StringVar(&opts.createConfig, "create-config", "", "Create a starter TOML config and exit: global (~/.klaudia/config.toml) or local (./.klaudia/config.toml)")
	f.BoolVar(&opts.loop, "loop", false, "Autonomous loop: iterate against the goal spec (PRD.md or .klaudia/GOAL.md) until complete or --max-iterations. Requires --dangerously-skip-permissions.")
	f.IntVar(&opts.maxIterations, "max-iterations", 0, "Max iterations for --loop (0 = default 10, hard cap 50)")

	return cmd
}

// run dispatches to headless or interactive mode. Phase 0 only implements a
// headless stub that emits a well-formed result so the output renderers and
// differential harness can be exercised end-to-end before the agent loop lands.
func run(cmd *cobra.Command, opts *options) error {
	format, err := ParseOutputFormat(opts.outputFormat)
	if err != nil {
		return err
	}

	cwd, _ := os.Getwd()
	if opts.createConfig != "" {
		path, err := createConfig(opts.createConfig, cwd)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n\nEdit it with your provider/baseURL/model/apiKeyEnv, then export the named API key env var and run klaudia.\n", path)
		return nil
	}

	// Mode: autonomous --loop | stream-json input (embedding) | headless -p |
	// interactive TUI. --loop is non-interactive and renders as text.
	interactive := !opts.print && !opts.loop && opts.inputFormat != "stream-json"
	if opts.loop {
		if opts.inputFormat == "stream-json" {
			return usageErrorf("--loop cannot be combined with --input-format stream-json")
		}
		if format != FormatText {
			return usageErrorf("--loop only supports --output-format text")
		}
	}
	if opts.print && format == FormatStreamJSON && !opts.verbose {
		return usageErrorf("--output-format stream-json requires --verbose")
	}
	if opts.partialMessages && (!opts.print || format != FormatStreamJSON) {
		return usageErrorf("--include-partial-messages only works with --print and --output-format=stream-json")
	}

	start := time.Now()
	ctx := cmd.Context()
	r := NewRenderer(format, cmd.OutOrStdout())

	// Resolve the session: explicit resume by id, auto-resume the most recent
	// project session by default, or start new when requested/no prior session.
	// --fork-session writes to a fresh id while preserving the original.
	var initialMessages []anthropic.BetaMessageParam
	resumeID, err := resolveResumeID(cwd, *opts, interactive)
	if err != nil {
		return err
	}
	// Auto-resume must not revive a session that ended in a model refusal:
	// its context keeps tripping the refusal, so every prompt in the resumed
	// session — even an unrelated one — refuses too. Start fresh instead. Only
	// for the implicit pick-the-most-recent case; an explicit --resume/--continue
	// is the user asking for that session by name, so it is honoured.
	autoResume := resumeID != "" && opts.resume == "" && !opts.continueSession
	if autoResume {
		if entries, rerr := session.Read(session.ExistingPath(cwd, resumeID)); rerr == nil && session.LastTurnRefused(entries) {
			fmt.Fprintln(cmd.ErrOrStderr(),
				"Last session ended in a model refusal — starting fresh. Its history is left on disk; --resume "+resumeID+" to see it.")
			resumeID = ""
		}
	}

	sessionID := uuid.NewString()
	if resumeID != "" {
		// Token-saving resume: if a persisted compaction summary exists and the
		// user didn't ask for a --full replay, seed from the summary instead of
		// the entire transcript. Falls back to full replay when no summary exists.
		if summary, ok := session.ReadSummary(cwd, resumeID); ok && !opts.fullResume {
			initialMessages = []anthropic.BetaMessageParam{
				anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(
					"Summary of the earlier conversation in this session:\n\n" + summary)),
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "resuming from compacted summary (--full for the entire transcript)")
		} else {
			entries, rerr := session.Read(session.ExistingPath(cwd, resumeID))
			if rerr != nil {
				return fmt.Errorf("resume %s: %w", resumeID, rerr)
			}
			initialMessages, rerr = agent.MessagesFromEntries(entries)
			if rerr != nil {
				return fmt.Errorf("resume %s: %w", resumeID, rerr)
			}
		}
		if !opts.forkSession {
			sessionID = resumeID // continue appending to the same transcript
		}
	}

	// Select the model provider (.klaudia/config.toml: anthropic | openai).
	cfg := config.Load(cwd)
	provider, providerModel, err := buildProvider(cfg)
	if err != nil {
		return err
	}

	// --model overrides the config/provider default.
	modelStr := opts.model
	if modelStr == "" {
		modelStr = providerModel
	}
	model := api.ResolveModel(modelStr)

	// Build allow/deny rules from config (.klaudia) + CLI flags.
	allowRules, err := permission.ParseRules(append(append([]string{}, cfg.Permissions.Allow...), opts.allowedTools...))
	if err != nil {
		return usageErrorf("--allowedTools/permissions.allow: %v", err)
	}
	denyRules, err := permission.ParseRules(append(append([]string{}, cfg.Permissions.Deny...), opts.disallowedTools...))
	if err != nil {
		return usageErrorf("--disallowedTools/permissions.deny: %v", err)
	}
	// The host gate. extraDirs is read at check time rather than captured, so a
	// directory added mid-session with /add-dir counts as project work on the
	// next tool call.
	hostPolicy, hostNotice := agent.ResolveHostPolicy(
		cfg.Trust.Mode, len(allowRules) > 0 || len(denyRules) > 0)

	// Resolve the permission mode: --permission-mode flag wins, else the config
	// default ([permissions] mode), else autonomous — but only when the host
	// gate is enforcing. Autonomous without an enforcing gate is
	// bypassPermissions wearing a friendlier name, so a session with trust off
	// or in observe keeps asking per action until the user upgrades.
	// --dangerously-skip wins over all.
	modeStr := opts.permissionMode
	if modeStr == "" {
		modeStr = cfg.Permissions.Mode
	}
	if modeStr == "" {
		if hostPolicy == agent.HostEnforce {
			modeStr = string(permission.ModeAutonomous)
		} else {
			modeStr = string(permission.ModeDefault)
		}
	}
	mode := permission.Mode(modeStr)
	if !mode.Valid() {
		return usageErrorf("invalid permission mode %q (autonomous|plan|bypassPermissions, or legacy default|acceptEdits|dontAsk)", modeStr)
	}
	if mode == permission.ModeAutonomous && hostPolicy != agent.HostEnforce {
		return usageErrorf("permission mode %q needs the host guardrail enforcing, but [trust] mode is %q — "+
			"autonomous without it would allow everything, which is what bypassPermissions is for",
			mode, hostPolicy)
	}
	if opts.dangerouslySkip {
		mode = permission.ModeBypassPermissions
	}

	// Headless path: the mode is fixed for the lifetime of this command.
	permCtx := permission.Context{Mode: permission.StaticMode(mode), Allow: allowRules, Deny: denyRules}
	var extraDirs func() []string
	hostGate := &agent.HostGate{
		Policy: hostPolicy,
		Roots: func() trust.Roots {
			home, _ := os.UserHomeDir()
			roots := []string{cwd}
			if extraDirs != nil {
				roots = append(roots, extraDirs()...)
			}
			return trust.NewRoots(home, roots...)
		},
		Ledger: trust.NewLedger(trust.NewRoots(func() string { h, _ := os.UserHomeDir(); return h }(), cwd)),
		// Refusals point the model at the tool that gets an operation approved
		// in one go, rather than leaving it to retry the command.
		DeclareTool: "RequestHostChange",
	}

	// Refresh MEMORY.md's links to the .klaudia/memory/*.md detail notes before
	// building the prompt, so recall surfaces them (best-effort; idempotent).
	_ = memory.New(filepath.Join(cwd, ".klaudia")).SyncLinks()
	// Assemble the full system prompt (base instructions + env context +
	// CLAUDE.md) once for this run.
	sysPrompt := prompt.System(cwd, string(model))

	// Build the tool registry. Sub-agents draw from the base tools (incl. any
	// MCP tools); the top-level registry adds the Agent tool.
	executor := buildExecutor(cfg.Sandbox, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) })
	// Lazy browser engine for the web tools, tied to the run context and closed
	// at session end so any launched Chrome is reliably terminated (it launches
	// nothing until a web tool actually runs).
	browserEngine := browser.NewEngine(ctx, buildBrowserOptions(cfg.Browser))
	defer browserEngine.Close()
	// Managed jobs (Bash run_in_background) are session-scoped and tied to the
	// run context; KillAll stops them — and now their whole process groups — at
	// session end. The session id names their log directory so /logs can find
	// them and so a resumed session lands on its own logs.
	jobStore := tools.NewJobStore(ctx, sessionID)
	defer jobStore.KillAll()
	// Lazy language-server pool for code-intel tools (Diagnostics/Definition/
	// References). Servers are detected on PATH + toolchain dirs, spawned on
	// first use, and shut down at session end. Not downloaded.
	lspPool := lsp.NewPool(ctx, cwd, cfg.LSP.Disabled, nil)
	defer lspPool.Close()
	base, err := tools.DefaultRegistry(executor,
		tools.WithBrowserEngine(browserEngine),
		tools.WithJobStore(jobStore),
		tools.WithLSP(lspPool),
	)
	if err != nil {
		return err
	}

	// Connect configured MCP servers (.mcp.json) and fold in their tools and
	// resource tools. Best effort: a server failure does not abort the run.
	mcpCfg, _ := mcp.LoadConfig(cwd)
	mcpMgr, mcpErrs := mcp.Connect(ctx, mcpCfg)
	defer mcpMgr.Close()
	for _, e := range mcpErrs {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", e)
	}
	baseTools := base.All()
	baseTools = append(baseTools, mcpMgr.Tools(ctx)...)
	if rts, rerr := mcpMgr.ResourceTools(); rerr == nil && len(mcpMgr.Servers()) > 0 {
		baseTools = append(baseTools, rts...)
	}
	// Persistent memory: one store shared by the Memory tool (agent + sub-agents)
	// and the /memory command.
	memStore := memory.New(filepath.Join(cwd, ".klaudia"))
	if memTool, merr := tools.NewMemoryForProject(memStore, cwd); merr == nil {
		baseTools = append(baseTools, memTool)
	}

	// User-defined skills (~/.klaudia/skills overlaid by .klaudia/skills) become a
	// single Skill tool the model can invoke; the TUI also dispatches /<skill>.
	skills := skill.Load(cwd, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) })
	skillInfos := skillToolInfos(skills)
	if skillTool, serr := tools.NewSkill(skillInfos); serr == nil && skillTool != nil {
		baseTools = append(baseTools, skillTool)
	}

	// Deferred tool loading: MCP tools can be numerous, so withhold them from
	// the initial request behind a ToolSearch tool that loads them on demand.
	deferredTools := map[string]bool{}
	for _, t := range baseTools {
		if strings.HasPrefix(t.Name(), "mcp__") {
			deferredTools[t.Name()] = true
		}
	}
	if len(deferredTools) > 0 {
		catalog := make([]tools.ToolInfo, 0, len(baseTools))
		for _, t := range baseTools {
			desc, _ := t.Description(ctx)
			catalog = append(catalog, tools.ToolInfo{Name: t.Name(), Description: desc})
		}
		if ts, terr := tools.NewToolSearch(catalog); terr == nil {
			baseTools = append(baseTools, ts)
		}
	}
	base = tools.NewRegistry(baseTools...)

	// Headless has no one to ask. Ordinary work still runs; host changes are
	// refused with the flag that would permit them, so the output says what to
	// do rather than only what failed.
	approver := agent.HeadlessApprover(opts.allowHostChanges)
	registry, err := withAgentTool(base, provider, model, permCtx, approver, opts.maxTurns, deferredTools, cwd, hostGate)
	if err != nil {
		return err
	}

	// Open the transcript for this session (best effort: a transcript failure
	// should not abort the run).
	var recorder agent.Recorder
	if tr, terr := session.NewTranscript(session.Meta{
		SessionID:      sessionID,
		CWD:            cwd,
		Version:        version.Version,
		GitBranch:      gitBranch(cwd),
		PermissionMode: string(mode),
	}); terr == nil {
		defer func() { _ = tr.Close() }()
		recorder = tr
	}

	loop := agent.New(provider, registry)

	// Persist compaction summaries for token-saving resume (a Klaudia divergence).
	onSummary := func(summary string) {
		_ = session.WriteSummary(cwd, sessionID, summary, gitCommit(cwd))
	}

	// Interactive TUI: the default when not headless and not stream-json input.
	// It drives the same loop, prompting the user to resolve permission asks.
	if interactive {
		// This config predates the trust model and already carries permission
		// rules, so the gate starts in observe: it reports what it finds and
		// changes nothing. Said once, at startup, because a behaviour change in
		// this particular area should not be discovered by accident.
		if hostNotice {
			fmt.Fprintln(cmd.ErrOrStderr(),
				"note: Klaudia now works autonomously inside the project and asks before changing this machine. "+
					"Your existing permission rules still apply, so this session only reports what the new model "+
					"would have done — run /trust to see it, or /trust upgrade to switch over.")
		}
		// Shared settings so slash commands can read/change them between turns.
		ctxLimit, ctxSource := api.ContextWindow(string(model), cfg.ContextWindow)
		sess := &tui.Session{
			// Effective model (flag or config default), never "" — otherwise a
			// non-Anthropic provider would wrongly resolve to the Anthropic default.
			SessionID:           sessionID,
			Model:               modelStr,
			ResolvedModel:       string(model),
			Theme:               themeOrWarn(cfg.Theme, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) }), // user default (~/.klaudia) overlaid by project; /theme overrides per session
			PermissionMode:      string(mode),
			Memory:              memStore,
			MCP:                 mcpController{mgr: mcpMgr, ctx: ctx},
			Skills:              tuiSkills(skills, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) }),
			Provider:            providerName(cfg),
			SandboxMode:         sandboxMode(cfg.Sandbox),
			CWD:                 cwd,
			GitBranch:           gitBranch(cwd),
			Agents:              tuiAgents(),
			ContextWindow:       ctxLimit,
			ContextWindowSource: ctxSource,
			Compact: func(ctx context.Context, history []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
				return compactAndPersist(ctx, history, func(ctx context.Context, history []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
					return loop.Compact(ctx, history, api.ResolveModel(modelStr))
				}, onSummary)
			},

			Doctor: func() string {
				return doctor.Format(doctor.Run(buildDoctorInput(cfg, model, cwd, len(mcpCfg.MCPServers))))
			},
			// Nil unless the provider can enumerate its models; /model falls
			// back to type-the-id when it is.
			ListModels: modelLister(provider),
			Trust:      tui.NewTrustController(hostGate),
			Jobs:       jobStore,
			Executor:   executor,
		}
		extraDirs = func() []string { return sess.ExtraDirs }
		runFn := func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, ap agent.Approver, asker tools.Asker, planner tools.Planner, emit agent.Emitter, interject func() agent.Interjection, beforeEdit func(string, []string)) (agent.Result, error) {
			// Permission mode reads live from the session every check, so a
			// /mode bypass (or ExitPlanMode flipping out of plan) takes effect
			// on the very next tool dispatch inside the agent loop — not just
			// at the next TUI turn boundary. The Context itself is rebuilt
			// here so the rule lists stay snapshot-stable for the duration of
			// this turn, but the mode probe is live.
			turnPerm := permission.Context{
				Mode:  func() permission.Mode { return permission.Mode(sess.PermissionMode) },
				Allow: allowRules,
				Deny:  denyRules,
			}
			return loop.Run(ctx, agent.Options{
				WorkingDir:      cwd,
				Prompt:          prompt,
				Model:           api.ResolveModel(sess.Model), // resolved fresh each turn
				System:          withExtraDirs(sysPrompt, sess.ExtraDirs),
				MaxTurns:        opts.maxTurns,
				ContextWindow:   cfg.ContextWindow,
				Permission:      turnPerm,
				Host:            hostGate,
				Interject:       interject,
				BeforeEdit:      beforeEdit,
				DeferredTools:   deferredTools,
				Approver:        ap,
				Asker:           asker,
				Planner:         planner,
				InitialMessages: history,
				Recorder:        recorder,
				WebTools:        true,
				OnSummary:       onSummary,
			}, emit)
		}
		return tui.Run(ctx, tui.RunFunc(runFn), initialMessages, sess)
	}

	// Stream-json input: drive a persistent agent over stdin/stdout (the
	// embedding channel). Each user message is a turn; permission asks are
	// surfaced as control_request and answered by the peer.
	if opts.inputFormat == "stream-json" {
		driver := streamjson.NewDriver(cmd.OutOrStdout())
		runFn := func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, ap agent.Approver, emit agent.Emitter) (agent.Result, error) {
			return loop.Run(ctx, agent.Options{
				WorkingDir:      cwd,
				Prompt:          prompt,
				Model:           model,
				System:          sysPrompt,
				MaxTurns:        opts.maxTurns,
				ContextWindow:   cfg.ContextWindow,
				Permission:      permCtx,
				Host:            hostGate,
				Approver:        ap,
				DeferredTools:   deferredTools,
				InitialMessages: history,
				Recorder:        recorder,
				WebTools:        true,
				OnSummary:       onSummary,
			}, emit)
		}
		return driver.Run(ctx, cmd.InOrStdin(), runFn)
	}

	// Autonomous goal loop: iterate against the spec until complete or capped.
	if opts.loop {
		return runGoalLoop(ctx, cmd, loopRun{
			loop:       loop,
			cwd:        cwd,
			mode:       mode,
			model:      model,
			system:     sysPrompt,
			maxTurns:   opts.maxTurns,
			iterations: opts.maxIterations,
			permCtx:    permCtx,
			hostGate:   hostGate,
			approver:   approver,
			deferred:   deferredTools,
			recorder:   recorder,
			onSummary:  onSummary,
			render:     r,
		})
	}

	// Single-shot headless run. For stream-json output, emit JS-compatible
	// message envelopes (assistant/user) via an envelope recorder alongside the
	// transcript; the simplified delta events are not used in that mode.
	runRecorder := agent.Recorder(recorder)
	emit := func(ev agent.Event) { _ = r.Event(ev) }
	var partial func(anthropic.BetaRawMessageStreamEventUnion)
	if format == FormatStreamJSON {
		// Serialize envelope + partial writes to the same stream.
		var writeMu sync.Mutex
		out := cmd.OutOrStdout()
		runRecorder = multiRecorder{recorder, newEnvelopeRecorder(out, sessionID)}
		emit = func(agent.Event) {}
		if opts.partialMessages {
			partial = newPartialEmitter(out, sessionID, &writeMu).emit
		}
	}
	res, err := loop.Run(ctx, agent.Options{
		WorkingDir:      cwd,
		Prompt:          opts.prompt,
		Model:           model,
		System:          sysPrompt,
		MaxTurns:        opts.maxTurns,
		ContextWindow:   cfg.ContextWindow,
		Permission:      permCtx,
		Host:            hostGate,
		Approver:        approver,
		DeferredTools:   deferredTools,
		InitialMessages: initialMessages,
		Recorder:        runRecorder,
		WebTools:        true,
		OnSummary:       onSummary,
		PartialMessages: partial,
	}, emit)

	out := ResultMessage{
		Type:          "result",
		Subtype:       "success",
		IsError:       err != nil,
		DurationMS:    time.Since(start).Milliseconds(),
		DurationAPIMS: time.Since(start).Milliseconds(),
		NumTurns:      res.NumTurns,
		Result:        res.Text,
		StopReason:    res.StopReason,
		SessionID:     sessionID,
		TotalCostUSD:  0, // Phase 3: derive from usage + pricing.
		Usage: map[string]any{
			"input_tokens":                res.InputTokens,
			"output_tokens":               res.OutputTokens,
			"cache_read_input_tokens":     res.CacheReadInputTokens,
			"cache_creation_input_tokens": res.CacheCreationInputTokens,
		},
		UUID: uuid.NewString(),
	}
	if err != nil {
		out.Subtype = "error_during_execution"
		out.Result = "Error: " + api.FriendlyError(err)
	} else if agent.TurnEndedEmpty(res.Text) {
		// The turn completed but produced no answer — a refusal, or a limit.
		// Reporting "success" with empty output would let a pipeline treat a
		// refusal as a done task. stop_reason already carries the detail; this
		// makes stdout non-empty and the exit code non-zero to match.
		if note := agent.TurnNote(res.StopReason, false); note != "" {
			out.Subtype = res.StopReason
			out.IsError = true
			out.Result = note
		}
	}
	if rerr := r.Result(out); rerr != nil {
		return rerr
	}
	// Exit codes an automation can branch on. The reason is already in the
	// result payload, so nothing more is printed — only the code differs.
	switch {
	case errors.Is(err, context.Canceled):
		return exitError{ExitInterrupted}
	case err != nil:
		return exitError{ExitError}
	case hostChangeWasBlocked(hostGate):
		// The task needed something on this machine and had no way to ask.
		// A caller can turn that into `--allow-host-changes` by itself; making
		// it indistinguishable from a model failure would leave them guessing.
		return exitError{ExitHostChangeBlocked}
	case res.StopReason == "max_turns":
		return exitError{ExitMaxTurns}
	case agent.TurnEndedEmpty(res.Text) && agent.TurnNote(res.StopReason, false) != "":
		// The model refused or hit a limit and returned nothing. No answer came
		// back, so this is a failure a caller should be able to branch on.
		return exitError{ExitError}
	}
	return nil
}

// usageErrorf reports an invocation mistake: a bad flag combination, an
// invalid mode, an unparseable rule. Nothing ran, and the caller should fix the
// command line rather than retry it.
func usageErrorf(format string, args ...any) error {
	return fmt.Errorf("%w%s", exitError{ExitUsage}, fmt.Sprintf(format, args...))
}

// errRendered marks a run error that has already been emitted in the result
// output, so Execute exits non-zero without re-printing it.
var errRendered = fmt.Errorf("run failed")

// Execute runs the root command, returning the process exit code.
// hostChangeWasBlocked reports whether the guardrail stopped anything this run.
func hostChangeWasBlocked(g *agent.HostGate) bool {
	for _, r := range g.Reports() {
		if r.Enforced {
			return true
		}
	}
	return false
}

func Execute() int { return ExecuteContext(context.Background()) }

// ExecuteContext runs the root command against ctx. Cancelling ctx (SIGINT /
// SIGTERM from main) unwinds the run, which tears down background jobs,
// browsers, MCP servers and the transcript through the existing defers.
func ExecuteContext(ctx context.Context) int {
	err := NewRootCommand().ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	// A bare exitError carries a code and no text, because the reason is
	// already in the result payload; errRendered likewise. A usage error wraps
	// an exitError *and* a message, and that message has not been seen yet —
	// so the test is whether there is anything to say, not what type it is.
	if msg := strings.TrimSpace(err.Error()); msg != "" && !errors.Is(err, errRendered) {
		fmt.Fprintln(os.Stderr, "Error:", msg)
	}
	return exitCodeFor(err)
}
