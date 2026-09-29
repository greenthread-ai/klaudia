package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/greenthread-ai/klaudia/internal/gitprobe"
	"github.com/pelletier/go-toml/v2"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/greenthread-ai/klaudia/internal/acp"
	"github.com/greenthread-ai/klaudia/internal/anthropicauth"
	"github.com/greenthread-ai/klaudia/internal/browser"
	"github.com/greenthread-ai/klaudia/internal/config"
	"github.com/greenthread-ai/klaudia/internal/doctor"
	"github.com/greenthread-ai/klaudia/internal/hooks"
	"github.com/greenthread-ai/klaudia/internal/hostgate"
	"github.com/greenthread-ai/klaudia/internal/lsp"
	"github.com/greenthread-ai/klaudia/internal/mcp"
	"github.com/greenthread-ai/klaudia/internal/memory"
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
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// resumeFromSummary rebuilds a compacted session as it stood live: the last
// compaction's summary, then every entry recorded after it. ok is false when
// the session was never compacted, which means a full replay.
//
// It used to seed from the summary alone and drop everything after it, so a
// restart lost all work since the last compaction — 572 entries, 80 minutes,
// in the session that found this.
//
// A transcript written before boundary lines existed is cut at the summary
// file's "_Compacted <ts>_" stamp instead; one with a summary file but no
// stamp keeps the old summary-only behaviour, as nothing better is knowable.
func resumeFromSummary(cwd, sessionID string) ([]anthropic.BetaMessageParam, bool, error) {
	path := session.ExistingPath(cwd, sessionID)
	rp, err := session.ReadForResume(path)
	if err != nil {
		return nil, false, err
	}
	summary, after := rp.Summary, rp.After
	if summary == "" {
		body, at, ok := session.ReadSummaryStamped(cwd, sessionID)
		if !ok {
			return nil, false, nil
		}
		summary = body
		if at == "" {
			after = nil
		} else {
			after = session.EntriesAfter(rp.After, at)
		}
	}
	rest, err := session.Messages(after)
	if err != nil {
		return nil, false, err
	}
	return append(compaction.ReplaceWithSummary(summary), rest...), true, nil
}

func compactAndPersist(ctx context.Context, history []anthropic.BetaMessageParam, compact tui.CompactFunc, onSummary func(string)) ([]anthropic.BetaMessageParam, string, error) {
	newHistory, summary, err := compact(ctx, history)
	if err == nil && onSummary != nil {
		onSummary(summary)
	}
	return newHistory, summary, err
}

// agentWiring is the registry the main loop dispatches from, plus the parts a
// config reload needs in order to rebuild it: the Agent tool to re-append, and
// the spawner whose deferred-tool set has to track the new tool list.
type agentWiring struct {
	registry  *tools.Registry
	spawner   *agent.Spawner
	agentTool tools.Tool
}

// withAgentTool returns a registry that is the base tools plus the Agent tool,
// wired to a sub-agent spawner that draws from the base tools.
func withAgentTool(base *tools.Registry, provider api.Provider, model anthropic.Model, perm permission.Context, approver agent.Approver, maxTurns int, deferred map[string]bool, workingDir string, host *hostgate.HostGate, worktrees bool) (*agentWiring, error) {
	spawner := agent.NewSpawnerWithDeferred(provider, base, model, perm, approver, maxTurns, deferred).
		WithTypes(subagentTypes).
		WithHints(config.Hints).
		WithWorkingDir(workingDir).
		WithGate(gateFor(host)).
		WithIsolator(isolator(worktrees))

	infos := make([]tools.AgentTypeInfo, 0)
	for _, t := range subagent.Builtin() {
		infos = append(infos, tools.AgentTypeInfo{Name: t.Name, Description: t.Description})
	}
	agentTool, err := tools.NewAgent(spawner, infos)
	if err != nil {
		return nil, err
	}
	return &agentWiring{
		registry:  tools.NewRegistry(append(base.All(), agentTool)...),
		spawner:   spawner,
		agentTool: agentTool,
	}, nil
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
			ArgHint:     sk.ArgHint,
			Render:      sk.Render,
		})
	}
	return infos
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
//
// hookRunner may be nil — the common case, meaning no hook is configured
// anywhere; doctor is told "none" rather than being left silent about a
// subsystem that runs shell commands when it is on.
//
// limits is passed in rather than derived here. Deriving it made /doctor the
// one place that could disagree with the run it is diagnosing: it re-resolved
// the cap and the window from the offline tables, so a model sized by the
// provider, or switched with /model, was reported at the table's figure (or the
// 8192 floor) while the turn asked for something else.
func buildDoctorInput(cfg config.Config, model anthropic.Model, limits api.ModelLimits, effort api.Effort, cwd string, mcpCfg mcp.Config, hookRunner *hooks.Runner) doctor.Input {
	servers, hints := lsp.Survey(cwd)
	// Loaded silently: skill.Load's warnings go to the session that owns the
	// prompt, not to /doctor, which must stay quiet on stderr.
	// Skills a backend syncs into ~/.claude/skills/synced are not loaded (see
	// skill.loadDir); counting them keeps them discoverable in /doctor rather
	// than silently absent.
	syncedIgnored := 0
	if home, err := os.UserHomeDir(); err == nil {
		matches, _ := filepath.Glob(filepath.Join(home, ".claude", "skills", "synced", "*", "*", "SKILL.md"))
		syncedIgnored = len(matches)
	}
	doctorSkills := make([]doctor.Skill, 0)
	for _, sk := range skill.Load(cwd, func(string) {}) {
		scope := "user"
		if strings.HasPrefix(sk.Path, cwd) {
			scope = "project"
		}
		doctorSkills = append(doctorSkills, doctor.Skill{Name: sk.Name, Scope: scope})
	}
	lspServers := make([]doctor.LSPServer, 0, len(servers))
	for _, s := range servers {
		lspServers = append(lspServers, doctor.LSPServer{Name: s.Bin, Language: s.Language, Version: s.Version})
	}
	links, linksWhy := tui.HyperlinksDecision(cfg.Links.Enabled, os.Getenv)
	in := doctor.Input{
		Hyperlinks:      links,
		HyperlinksWhy:   linksWhy,
		Provider:        providerName(cfg),
		Model:           string(model),
		SandboxMode:     sandboxMode(cfg.Sandbox),
		ConfigFound:     configFileExists(cwd),
		MCPServers:      len(mcpCfg.MCPServers),
		MCPLegacySSE:    legacySSEServers(mcpCfg),
		LSPServers:      lspServers,
		Skills:          doctorSkills,
		SyncedIgnored:   syncedIgnored,
		Hooks:           doctorHooks(hookRunner),
		MissingLSPHints: hints,
		AuthKind:        "none",
		ContextWindow:   limits.ContextWindow,
		ContextSource:   limits.ContextSource,
		Effort:          string(effort),
		MaxOutputTokens: limits.MaxOutputTokens,
		MaxOutputSource: limits.MaxOutputSource,

		TableMaxOutputTokens: limits.TableMaxOutputTokens,
		TableContextWindow:   limits.TableContextWindow,
	}
	// Read-only: the cache is only consulted for its age, never refreshed here.
	cache := api.NewModelCache(nil, api.ModelCachePath(session.ConfigRoot(), providerName(cfg), resolveEndpoint(cfg)))
	in.ModelsFetchedAt, in.ModelsCount, in.ModelsCached = cache.FetchedAt()
	if cfg.Provider == config.ProviderOpenAI {
		if cfg.ResolveAPIKey() != "" {
			in.AuthOK, in.AuthKind = true, "api-key"
		}
	} else if cred, err := anthropicauth.ResolveCredential(); err == nil {
		in.AuthOK = true
		if cred.IsOAuth() {
			in.AuthKind = "oauth"
		} else {
			in.AuthKind = "api-key"
		}
	}
	return in
}

// doctorHooks flattens the session's hooks for the report.
func doctorHooks(r *hooks.Runner) []doctor.Hook {
	all := r.All()
	out := make([]doctor.Hook, 0, len(all))
	approved := r.ProjectApproved()
	for _, h := range all {
		scope := "user"
		if h.Project {
			scope = "project"
		}
		out = append(out, doctor.Hook{
			Event:   string(h.Event),
			Matcher: h.Matcher(),
			Scope:   scope,
			Dormant: h.Project && !approved,
		})
	}
	return out
}

// legacySSEServers names the configured MCP servers still pinned to the
// deprecated HTTP+SSE transport, sorted so /doctor reads the same every run.
func legacySSEServers(cfg mcp.Config) []string {
	var out []string
	for name, sc := range cfg.MCPServers {
		if strings.EqualFold(strings.TrimSpace(sc.Type), "sse") {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// configFileExists reports whether a home or project .klaudia/config.toml exists.
func configFileExists(cwd string) bool {
	if _, err := os.Stat(config.UserPath()); err == nil {
		return true
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
		if tui.IsBuiltinCommand(sk.Name) {
			warn(fmt.Sprintf("skill %q shadows built-in /%s; reachable only via the Skill tool", sk.Name, sk.Name))
		}
		out = append(out, tui.SkillCommand{Name: sk.Name, Description: sk.Description, Render: sk.Render})
	}
	return out
}

// acpCommands adapts loaded skills into the /commands an ACP client offers in
// its command picker.
//
// Skills only, and no shadowing check: ACP has no built-in commands of its own
// to collide with, because Klaudia's own slash commands live in the TUI and
// cannot run from here (see internal/acp/commands.go).
//
// The hint is set only for skills that actually interpolate $ARGUMENTS. A client
// renders it as placeholder text, so offering "arguments" for a skill that takes
// none invites the user to type something the skill would then append to its own
// body as a stray trailing line.
func acpCommands(skills []skill.Skill) []acp.Command {
	out := make([]acp.Command, 0, len(skills))
	for _, sk := range skills {
		sk := sk
		c := acp.Command{Name: sk.Name, Description: sk.Description, Render: sk.Render}
		if strings.Contains(sk.Body, "$ARGUMENTS") {
			c.Hint = "arguments"
		}
		out = append(out, c)
	}
	return out
}

// acpTranscript opens one transcript per ACP session.
//
// Best effort, matching every other transcript call site: a record that cannot
// be opened is reported to the client's log and the session runs unrecorded,
// because losing the history is not a reason to refuse the work.
//
// It takes the resolved sizing because an ACP session's recorder is installed
// per session via Turn.Recorder, which overrides the process-wide one — so the
// SetSizing that baseOptions performs never reaches these transcripts and they
// would be the one frontend recording a turn without its figures. Stamped at
// construction rather than per turn because ACP has no /model: the model is
// fixed for the life of the process.
func acpTranscript(cwd string, mode permission.Mode, model string, limits api.ModelLimits) func(string) acp.Transcript {
	return func(sessionID string) acp.Transcript {
		tr, err := session.NewTranscript(session.Meta{
			SessionID:      sessionID,
			CWD:            cwd,
			Version:        version.Version,
			GitBranch:      gitBranch(cwd),
			PermissionMode: string(mode),
		})
		if err != nil {
			return nil
		}
		tr.SetSizing(model, limits.MaxOutputTokens, limits.MaxOutputSource, limits.ContextWindow, limits.ContextSource)
		return tr
	}
}

// acpLoadHistory reads a persisted session back for session/load.
//
// The full transcript, not the compacted summary --resume prefers: the client is
// reopening a conversation to *show* it, and a summary would replay four
// paragraphs of prose in place of the thread the user is looking for. The token
// cost of the longer history is the same cost --resume --full pays.
func acpLoadHistory(cwd string) func(string) ([]anthropic.BetaMessageParam, error) {
	return func(sessionID string) ([]anthropic.BetaMessageParam, error) {
		entries, err := session.Read(session.ExistingPath(cwd, sessionID))
		if err != nil {
			return nil, fmt.Errorf("no session %s in this project: %w", sessionID, err)
		}
		return session.Messages(entries)
	}
}

// acpSessions lists the project's persisted sessions for session/list.
func acpSessions(cwd string) []acp.SessionSummary {
	all := session.List(cwd)
	out := make([]acp.SessionSummary, 0, len(all))
	for _, s := range all {
		out = append(out, acp.SessionSummary{
			ID:        s.ID,
			Title:     s.Title,
			UpdatedAt: s.Modified.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// modelLister exposes the provider's model enumeration to the TUI when it has
// one. Both shipped providers do, but the capability is an optional interface
// rather than part of Provider — so a future backend that can't list models
// still satisfies Provider, and /model degrades to accepting a typed id.
// modelLister exposes the provider's model enumeration to the TUI when it has
// one. Both shipped providers do, but the capability is an optional interface
// rather than part of Provider — so a future backend that can't list models
// still satisfies Provider, and /model degrades to accepting a typed id.
// limitsResolver is the sizing resolver /model uses: the startup one, with the
// config overrides bound in and the network off. /model used to size a model
// on its own — opposite precedence, and no config at all, so a maxTokens set in
// .klaudia/config.toml silently stopped applying after any switch. Off the
// network because it runs on the UI goroutine; the picker has just listed the
// models through the cache, so a fresh answer is on disk anyway.
func limitsResolver(lister api.ModelLister, cfgMaxTokens, cfgContextWindow int) func(string, api.ModelInfo) api.ModelLimits {
	return func(model string, known api.ModelInfo) api.ModelLimits {
		return api.Resolve(context.Background(), api.LimitsInput{
			Model: model, Lister: lister, Known: known, NoNetwork: true,
			ConfigMaxTokens: cfgMaxTokens, ConfigContextWindow: cfgContextWindow,
		})
	}
}

// cachedModelLister prefers the cache, so listing models refreshes it.
func cachedModelLister(c *api.ModelCache, p api.Provider) func(context.Context) ([]api.ModelInfo, error) {
	if c != nil {
		return c.ListModels
	}
	return modelLister(p)
}

func modelLister(p api.Provider) func(context.Context) ([]api.ModelInfo, error) {
	lister, ok := p.(api.ModelLister)
	if !ok {
		return nil
	}
	return lister.ListModels
}

// resolveEndpoint returns the API endpoint for the active provider:
// KLAUDIA_CUSTOM_ENDPOINT wins, else the configured baseURL — env over config,
// as everywhere else. The env var used to reach only the Anthropic client; it
// now applies to whichever dialect is selected, so one override behaves the
// same way on both.
func resolveEndpoint(cfg config.Config) string {
	return firstNonEmpty(os.Getenv("KLAUDIA_CUSTOM_ENDPOINT"), cfg.ResolveBaseURL())
}

// apiTuning maps the config's transport settings onto the providers'.
func apiTuning(cfg config.Config) api.Tuning {
	t := api.Tuning{MaxRetries: cfg.MaxRetries}
	if cfg.StreamIdleTimeout != nil {
		d := time.Duration(*cfg.StreamIdleTimeout) * time.Second
		t.StreamIdleTimeout = &d
	}
	return t
}

// buildProvider selects and constructs the model provider from config. It
// returns the provider and the provider's default model. Anthropic is the
// default; "openai" uses an OpenAI-compatible Chat Completions endpoint.
func buildProvider(cfg config.Config) (api.Provider, string, error) {
	endpoint := resolveEndpoint(cfg)
	switch cfg.Provider {
	case config.ProviderOpenAI:
		if endpoint == "" {
			return nil, "", fmt.Errorf("provider \"openai\" requires baseURL in ~/.klaudia/config.toml (try --create-config=global); a project config cannot set it")
		}
		key := cfg.ResolveAPIKey()
		if key == "" {
			return nil, "", fmt.Errorf("provider \"openai\" needs apiKey or apiKeyEnv in ~/.klaudia/config.toml or ./.klaudia/config.toml; if using apiKeyEnv, export that variable before running")
		}
		return api.NewOpenAIProvider(endpoint, key, cfg.Temperature, apiTuning(cfg)), cfg.Model, nil
	default:
		cred, err := anthropicauth.ResolveCredential()
		if err != nil {
			return nil, "", err
		}
		return anthropicauth.NewClient(cred, endpoint, apiTuning(cfg)), cfg.Model, nil
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
	// rebuild re-reads the live MCP tools into the registries. A server that
	// failed at startup contributed no tools, so reconnecting it without a
	// rebuild left it connected and unusable for the rest of the session.
	rebuild func()
	// The project servers held for approval: the trust that holds them, the
	// whole config they are judged against, and the step that starts them.
	trust  *mcp.ProjectTrust
	latest func() mcp.Config
	start  func() []error
}

func (c mcpController) Servers() []tui.MCPServerInfo {
	// Count live tools per server, by the server each tool reports — its name
	// may have been rewritten to fit the API, so it is not parsed.
	counts := map[string]int{}
	for _, t := range c.mgr.Tools(c.ctx) {
		if st, ok := t.(interface{ ServerName() string }); ok {
			counts[st.ServerName()]++
		}
	}
	out := make([]tui.MCPServerInfo, 0)
	for _, s := range c.mgr.Servers() {
		out = append(out, tui.MCPServerInfo{Name: s.Name, Connected: s.Connected(), Tools: counts[s.Name]})
	}
	// Held servers are not in the manager at all, so without these a project
	// whose servers all await approval read as having none configured.
	if c.latest != nil {
		lines, refused := c.trust.Pending(c.latest())
		for _, l := range lines {
			name, detail, _ := strings.Cut(l, ": ")
			out = append(out, tui.MCPServerInfo{Name: name, Held: true, Refused: refused, Detail: detail})
		}
	}
	return out
}

func (c mcpController) ApproveProject() error {
	if c.latest == nil {
		return nil
	}
	if err := c.trust.Grant(c.latest()); err != nil {
		// Granted for this session even so; only the memory of it failed.
		c.start()
		return fmt.Errorf("started, but the approval was not saved (%w); you will be asked again next session", err)
	}
	return errors.Join(c.start()...)
}

func (c mcpController) Reconnect(name string) error {
	if err := c.mgr.Reconnect(name); err != nil {
		return err
	}
	if c.rebuild != nil {
		c.rebuild()
	}
	return nil
}

func (c mcpController) Disconnect(name string) error { return c.mgr.Disconnect(name) }

// turnSettings is the part of a run that one frontend changes between turns and
// another pins for the whole process. The TUI reads Model and ExtraDirs live
// from its Session so /model and /add-dir take effect on the next turn. Every
// non-interactive mode resolves them once at startup.
//
// Permission is here for the frontends with one fixed mode. The two that vary
// it — the TUI and ACP, where the mode is per editor session — supply it on the
// Turn instead, which also makes it live: see agent.Turn.Mode.
//
// It exists so that baseOptions can be shared: these were the only fields the
// per-mode options blocks actually disagreed about, and keeping a whole
// duplicated block per mode in order to vary three values is what let the other
// dozen drift apart unnoticed.
type turnSettings struct {
	Model      anthropic.Model
	Permission permission.Context
	ExtraDirs  []string
	Effort     api.Effort
	// MaxTokens and ContextWindow are the resolved sizing for Model, from
	// api.ResolveLimits. They are here rather than read from cfg inside
	// baseOptions because they depend on the model, and the TUI's model is live
	// — reading cfg meant a /model switch to a model with a different cap kept
	// the old one, and that ContextWindow never reached the loop's autocompact
	// threshold at all.
	MaxTokens     int
	ContextWindow int
	// MaxTokensSource and ContextSource are the matching api.LimitSource*
	// labels. They are carried alongside the figures purely so the transcript
	// can record whether each number was authoritative or the unknown-model
	// floor — the figure alone does not say, and 8192 looks like a deliberate
	// cap rather than a missing table entry.
	MaxTokensSource string
	ContextSource   string
}

// resolveEffort applies the precedence --effort > KLAUDIA_EFFORT > config.
// The env var sits in the middle so a shell default can be overridden by a
// flag for one run, and a config file can be overridden by the shell.
func resolveEffort(flag, fromConfig string) (api.Effort, error) {
	for _, v := range []string{flag, os.Getenv("KLAUDIA_EFFORT"), fromConfig} {
		if strings.TrimSpace(v) == "" {
			continue
		}
		e, err := api.ParseEffort(v)
		if err != nil {
			return api.EffortUnset, usageErrorf("%s", err)
		}
		return e, nil
	}
	return api.EffortUnset, nil
}

// mcpReloadNotifier carries reload outcomes from the config watcher to whichever
// frontend is listening.
//
// The two run on different goroutines and the watcher starts first — it is
// wired before the frontend exists — so the listener is registered late and the
// emit side tolerates there being nobody home.
type mcpReloadNotifier struct {
	mu sync.Mutex
	fn func(mcp.ReloadEvent)
}

func (n *mcpReloadNotifier) register(fn func(mcp.ReloadEvent)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.fn = fn
}

func (n *mcpReloadNotifier) emit(ev mcp.ReloadEvent) {
	n.mu.Lock()
	fn := n.fn
	n.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// gitBranch returns the current git branch for dir, or "" if not a repo.
func gitBranch(dir string) string {
	cmd := gitprobe.Command(dir, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCommit returns the short HEAD commit hash, or "" outside a repo.
func gitCommit(dir string) string {
	cmd := gitprobe.Command(dir, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

const starterConfig = `# Klaudia config: ~/.klaudia/config.toml
# A project's ./.klaudia/config.toml overlays this, but may only tighten it:
# provider, endpoint, keys and anything that loosens permissions or the
# sandbox are yours alone to set, here.

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
# theme = "nord" # klaudia (default) | dracula | gruvbox | tokyo-night | nord | light | catppuccin

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
#
# [subagents]
# worktree = true # a writing sub-agent gets its own git checkout (default)
`

// starterProjectConfig is what --create-config=local writes: only what a
// project config may set. It arrives with the repository, so it can tighten
// the user's config and never loosen it (config.projectSafe).
const starterProjectConfig = `# Klaudia project config: ./.klaudia/config.toml
# Overlays ~/.klaudia/config.toml for this repository. It travels with the
# repo, so it may only tighten: provider, baseURL and keys, bypass mode, a
# looser sandbox, extra writable directories and a wider network allowlist
# are ignored here (with a warning) and belong in ~/.klaudia/config.toml.

# model = "sonnet"
# effort = "high"   # low | medium | high | xhigh | max
# theme = "nord"

# Stricter than the user's settings, for everyone working in this repo:
# [permissions]
# mode = "plan"
#
# [sandbox]
# mode = "os"                 # applies where the user has not chosen one
# egress = ["github.com"]     # narrows the user's allowlist, or sets one
`

func createConfig(scope, cwd string) (string, error) {
	var path string
	switch scope {
	case "global":
		path = config.UserPath()
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
	body := starterConfig
	if scope == "local" {
		body = starterProjectConfig
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
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
	fallbackModels   []string
	restricted       bool
	login            bool
	effort           string
	outputFormat     string
	inputFormat      string
	permissionMode   string
	allowHostChanges bool
	verbose          bool
	maxTurns         int
	resume           string // --resume <session-id>
	continueSession  bool   // --continue
	newSession       bool   // --new-session
	forkSession      bool   // --fork-session
	fullResume       bool   // --full (replay entire transcript, not the summary)

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
		if !session.ValidID(opts.resume) {
			return "", fmt.Errorf("--resume %q is not a session id (letters, digits, '.', '-' and '_' only)", opts.resume)
		}
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
		Version:       version.Full(),
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
	f.BoolVar(&opts.restricted, "restricted", false, "Run without this machine's state: an empty config root (no user config, global instructions, global MCP, skills, approvals or session history), no memory and no hooks — for evals and reproducible runs")
	f.StringSliceVar(&opts.fallbackModels, "fallback-model", nil, "Model to continue on when the model is overloaded or the provider fails (repeat or comma-separate for a chain; default: config fallbackModels)")
	f.StringVar(&opts.effort, "effort", "", "How hard the model works per response: "+api.EffortNames()+" (default: the provider's own, usually high)")
	f.StringVar(&opts.outputFormat, "output-format", "text", "Output format: text|json|stream-json")
	f.StringVar(&opts.inputFormat, "input-format", "text", "Input format: text|stream-json|acp (stream-json and acp both drive a persistent agent over stdin; acp speaks the Agent Client Protocol for editors such as Zed)")
	f.StringVar(&opts.permissionMode, "permission-mode", "", "Permission mode: autonomous|plan|bypassPermissions (default: config [permissions] mode, else autonomous)")
	f.BoolVar(&opts.allowHostChanges, "allow-host-changes", false, "Non-interactive runs: permit changes to this machine (packages, services, /etc, …) without a human to approve them")
	f.BoolVar(&opts.verbose, "verbose", false, "Verbose output (required for stream-json)")
	f.IntVar(&opts.maxTurns, "max-turns", 0, "Limit the number of agentic loop turns (0 = unlimited)")
	f.StringVarP(&opts.resume, "resume", "r", "", "Resume a session by ID")
	f.BoolVar(&opts.continueSession, "continue", false, "Resume the most recent session in this directory (default when available)")
	f.BoolVar(&opts.newSession, "new-session", false, "Start a fresh session instead of auto-resuming the most recent session in this directory")
	f.BoolVar(&opts.forkSession, "fork-session", false, "When resuming, start a new session ID (preserves the original)")
	f.BoolVar(&opts.fullResume, "full", false, "When resuming, replay the entire transcript instead of the compacted summary")
	f.BoolVar(&opts.partialMessages, "include-partial-messages", false, "Include partial message chunks as they arrive (only with --print and --output-format=stream-json)")
	f.BoolVar(&opts.login, "login", false, "Store an Anthropic API key for Klaudia (prompted without echo, or piped on stdin) in credentials.json under ~/.klaudia, and exit")
	f.StringVar(&opts.createConfig, "create-config", "", "Create a starter TOML config and exit: global (~/.klaudia/config.toml) or local (./.klaudia/config.toml)")
	f.BoolVar(&opts.loop, "loop", false, "Autonomous loop: iterate against the goal spec (PRD.md or .klaudia/GOAL.md) until complete or --max-iterations. Runs unattended, so it needs a mode that does not ask: autonomous (the default) or bypassPermissions; host changes also need --allow-host-changes.")
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
	// --restricted: every user-level file Klaudia reads or writes lives under
	// the config root, so pointing that at an empty directory for this
	// process removes them all at once — user config, global AGENTS.md and
	// .mcp.json, skills, hook and MCP approvals, transcripts, job logs —
	// rather than a switch per subsystem that the next subsystem forgets.
	// Memory and hooks are off as well, project ones included: both carry
	// state from earlier runs or execute it. How to reach the model — the
	// user config's provider, endpoint, key and model (config.Connection) — is
	// carried into the empty root: it is not state, and a project config may
	// not supply it. Environment credentials and the keychain login work too.
	if opts.restricted {
		conn := config.Connection()
		dir, err := os.MkdirTemp("", "klaudia-restricted-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		// The --login store is a credential, not state: carried too.
		if b, err := os.ReadFile(anthropicauth.LoginPath()); err == nil {
			if err := os.WriteFile(filepath.Join(dir, "credentials.json"), b, 0o600); err != nil {
				return err
			}
		}
		if b, err := toml.Marshal(conn); err == nil && len(b) > 0 {
			// 0600: it may hold an inline apiKey.
			if err := os.WriteFile(filepath.Join(dir, "config.toml"), b, 0o600); err != nil {
				return err
			}
		}
		if err := os.Setenv("KLAUDIA_CONFIG_DIR", dir); err != nil {
			return err
		}
		// For the readers outside the config root (~/.claude skills and
		// CLAUDE.md): see session.Restricted.
		if err := os.Setenv("KLAUDIA_RESTRICTED", "1"); err != nil {
			return err
		}
	}

	if opts.login {
		return login(cmd)
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

	// Mode: autonomous --loop | an embedding protocol on stdin (stream-json or
	// ACP) | headless -p | interactive TUI. --loop is non-interactive and
	// renders as text.
	embedded := opts.inputFormat == "stream-json" || opts.inputFormat == "acp"
	interactive := !opts.print && !opts.loop && !embedded
	// attended is the other question, and it is not the same one: whether a
	// human is reachable to answer something. An embedding peer has a user
	// sitting in front of it; it just does not have a terminal of ours. Asking
	// "interactive?" when the real question was "is anyone there?" is why MCP
	// elicitation was advertised to the TUI alone.
	attended := interactive || embedded
	switch opts.inputFormat {
	case "text", "stream-json", "acp":
	default:
		return usageErrorf("--input-format must be text, stream-json or acp")
	}
	if opts.loop {
		if embedded {
			return usageErrorf("--loop cannot be combined with --input-format %s", opts.inputFormat)
		}
		if format != FormatText {
			return usageErrorf("--loop only supports --output-format text")
		}
	}
	if opts.inputFormat == "acp" {
		if format != FormatText {
			// ACP owns stdout: every byte on it is a JSON-RPC frame. A renderer
			// writing there too would interleave with the protocol and the
			// editor would see a parse error rather than an explanation.
			return usageErrorf("--input-format acp does not use --output-format; it speaks JSON-RPC on stdout")
		}
		if opts.print {
			return usageErrorf("--input-format acp cannot be combined with --print: ACP is a persistent session, not a single shot")
		}
	}
	if opts.print && format == FormatStreamJSON && !opts.verbose {
		return usageErrorf("--output-format stream-json requires --verbose")
	}
	if opts.partialMessages && (!opts.print || format != FormatStreamJSON) {
		return usageErrorf("--include-partial-messages only works with --print and --output-format=stream-json")
	}
	// Single-shot -p takes its prompt from the command line, piped stdin, or
	// both; --loop reads a goal spec and stream-json input owns stdin.
	if opts.print && !opts.loop && opts.inputFormat != "stream-json" {
		if opts.prompt, err = resolvePrintPrompt(opts.prompt, cmd.InOrStdin(), pipedStdinWait, cmd.ErrOrStderr()); err != nil {
			return err
		}
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
	// Lifecycle hooks, if any are configured. Nil when neither config file
	// declares one, which every hook call site treats as "do nothing".
	hookRunner := hooks.Load(cwd, sessionID)
	if opts.restricted {
		hookRunner = nil
	}
	if resumeID != "" {
		// Token-saving resume: if a persisted compaction summary exists and the
		// user didn't ask for a --full replay, seed from the summary instead of
		// the entire transcript. Falls back to full replay when no summary exists.
		if seeded, ok, rerr := resumeFromSummary(cwd, resumeID); rerr != nil {
			return fmt.Errorf("resume %s: %w", resumeID, rerr)
		} else if ok && !opts.fullResume {
			initialMessages = seeded
			fmt.Fprintln(cmd.ErrOrStderr(), "resuming from compacted summary (--full for the entire transcript)")
		} else {
			entries, rerr := session.Read(session.ExistingPath(cwd, resumeID))
			if rerr != nil {
				return fmt.Errorf("resume %s: %w", resumeID, rerr)
			}
			initialMessages, rerr = session.Messages(entries)
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
	for _, w := range cfg.Ignored {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w)
	}
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

	// How big a request to make for this model. Resolved once here, where the
	// provider and the model are both known, and handed to every frontend
	// through turnSettings — the per-mode options blocks must not each work it
	// out, which is how the loop ended up reading cfg.ContextWindow while the
	// TUI status bar read a different number.
	//
	// The provider's figures win over the offline table, read from a
	// once-a-day cache of its model list so startup stays offline. A stale
	// cache refreshes in the background for the next run; only a model the
	// table does not know pays a lookup now. The TUI re-resolves on /model
	// through the same resolver (resolveLimitsFor), config included.
	var lister api.ModelLister
	var modelCache *api.ModelCache
	if l, ok := provider.(api.ModelLister); ok {
		modelCache = api.NewModelCache(l, api.ModelCachePath(session.ConfigRoot(), providerName(cfg), resolveEndpoint(cfg)))
		lister = modelCache
	}
	resolveLimitsFor := limitsResolver(lister, cfg.MaxTokens, cfg.ContextWindow)
	limits := api.Resolve(ctx, api.LimitsInput{Model: modelStr, Lister: lister, ConfigMaxTokens: cfg.MaxTokens, ConfigContextWindow: cfg.ContextWindow})
	if modelCache != nil && modelCache.Stale() {
		go func() {
			rctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = modelCache.ListModels(rctx)
		}()
	}

	// Effort: --effort wins, then KLAUDIA_EFFORT, then config. Unset stays
	// unset and sends nothing, so the provider's own default applies.
	effort, err := resolveEffort(opts.effort, cfg.Effort)
	if err != nil {
		return err
	}
	var maxEffort api.Effort
	if cfg.MaxEffort != "" {
		if maxEffort, err = api.ParseEffort(cfg.MaxEffort); err != nil {
			return usageErrorf("maxEffort: %s", err)
		}
		effort = api.CapEffort(string(model), effort, maxEffort)
	}
	// Refuse a level this model rejects rather than letting the first turn die
	// on a 400: the API errors on an unsupported combination instead of
	// dropping to the nearest level it does support.
	if msg := api.EffortError(string(model), effort); msg != "" {
		return usageErrorf("%s", msg)
	}

	// Resolve the permission mode: --permission-mode flag wins, else the config
	// default ([permissions] mode), else autonomous. --dangerously-skip wins
	// over all.
	modeStr := opts.permissionMode
	if modeStr == "" {
		modeStr = cfg.Permissions.Mode
	}
	if modeStr == "" {
		modeStr = string(permission.ModeAutonomous)
	}
	mode := permission.Mode(modeStr)
	if !mode.Valid() {
		return usageErrorf("invalid permission mode %q (autonomous|plan|bypassPermissions)", modeStr)
	}

	var extraDirs func() []string
	hostGate := &hostgate.HostGate{
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
	// Headless path: the mode is fixed for the lifetime of this command.
	permCtx := permission.Context{Mode: permission.StaticMode(mode)}

	// Refresh MEMORY.md's links to the .klaudia/memory/*.md detail notes before
	// building the prompt, so recall surfaces them (best-effort; idempotent).
	_ = memory.New(filepath.Join(cwd, ".klaudia")).SyncLinks()
	// Assemble the full system prompt (base instructions + env context +
	// AGENTS.md/CLAUDE.md) once for this run.
	// The model line is added per turn (baseOptions), not here: /model
	// switches it, and this string is built once.
	sysPrompt := prompt.System(cwd, "")

	// Build the tool registry. Sub-agents draw from the base tools (incl. any
	// MCP tools); the top-level registry adds the Agent tool.
	executor := buildExecutor(cfg.Sandbox, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) })
	egressProxy := startEgress(ctx, cfg.Sandbox, executor, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) })
	// Lazy browser engine for the web tools, tied to the run context and closed
	// at session end so any launched Chrome is reliably terminated (it launches
	// nothing until a web tool actually runs).
	browserEngine := browser.NewEngine(ctx, buildBrowserOptions(cfg.Browser))
	defer browserEngine.Close()
	// Managed jobs (Bash run_in_background) are session-scoped and tied to the
	// run context; KillAll stops them — and now their whole process groups — at
	// session end. The session id names their log directory so /logs can find
	// them and so a resumed session lands on its own logs.
	jobStore := tools.NewJobStore(ctx, filepath.Join(session.ConfigRoot(), "jobs"), sessionID)
	defer jobStore.KillAll()
	// A job that ends on its own is news for the model, in every frontend.
	// The TUI installs its own handler (a steering message, or a turn when
	// idle). Everywhere else exits queue in jobInbox, which each turn polls
	// through Turn.Interject, and jobExitHook lets stream-json tell its peer
	// between turns. Set before any job can start, so it needs no lock.
	jobInbox := &agent.Inbox{}
	var jobExitHook func(tools.JobStatus)
	var jobMatchHook func(tools.JobStatus, int)
	if !interactive {
		jobStore.OnExit(func(st tools.JobStatus) {
			if st.Stopped {
				return
			}
			jobInbox.Add(jobStore.ExitNotice(st))
			if jobExitHook != nil {
				jobExitHook(st)
			}
		})
		jobStore.OnMatch(func(st tools.JobStatus, lines []string, dropped int) {
			jobInbox.Add(jobStore.MatchNotice(st, lines, dropped))
			if jobMatchHook != nil {
				jobMatchHook(st, len(lines)+dropped)
			}
		})
	}
	// Where clamped tool output is kept in full (24h). The core no longer
	// picks these directories itself — an embedded agent must not decide on
	// its own to write under ~/.klaudia — so the host names both here.
	spill := tools.SpillStore{Dir: filepath.Join(session.ConfigRoot(), "outputs")}
	// Lazy language-server pool for code-intel tools (Diagnostics/Definition/
	// References). Servers are detected on PATH + toolchain dirs, spawned on
	// first use, and shut down at session end. Not downloaded.
	lspPool := lsp.NewPool(ctx, cwd, cfg.LSP.Disabled, nil)
	defer lspPool.Close()
	// The session plan, held here rather than inside the registry so a
	// frontend can render it. TodoWrite writes the step list to it and
	// ExitPlanMode writes the approved plan through to
	// ~/.klaudia/plans/<session-id>.md — keyed by session so two sessions in
	// one repo cannot overwrite each other's plan, and resolved after the
	// resume branch above so --resume keeps writing the same file.
	planStore := plan.New(plan.PathFor(session.ConfigRoot(), sessionID))
	base, err := tools.DefaultRegistry(executor,
		tools.WithBrowserEngine(browserEngine),
		tools.WithJobStore(jobStore),
		tools.WithLSP(lspPool),
		tools.WithPlanStore(planStore),
	)
	if err != nil {
		return err
	}

	// Connect configured MCP servers (.mcp.json) and fold in their tools and
	// resource tools. Best effort: a server failure does not abort the run.
	// A .mcp.json that does not parse contributes no servers (the other files
	// still do). Discarding that error made the session look like one with
	// that server unconfigured — the model reports it is down, and nothing
	// says why.
	mcpCfg, mcpCfgErr := mcp.LoadConfig(cwd)
	if mcpCfgErr != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: mcp config:", mcpCfgErr)
	}
	// Servers log to stderr. Headless and embedded runs forward it, prefixed
	// with the server name, to Klaudia's own stderr (which an embedder drains);
	// the TUI owns the terminal, so interactive runs do not.
	// KLAUDIA_MCP_STDERR=<dir> also keeps a per-server log in any mode.
	if !interactive {
		mcp.SetStderr(cmd.ErrOrStderr())
		defer mcp.SetStderr(nil)
	}
	// Only an attended run gets an elicitor. It is what makes Klaudia advertise
	// the elicitation capability, and a server told the user can be asked for an
	// API key will wait for one that is never coming when there is no frontend
	// to ask. Unattended servers see no capability and take their own
	// non-interactive path. Note this is `attended`, not `interactive`: the
	// stream-json peer can put a question to someone.
	var elicitor *mcp.Elicitor
	if attended {
		elicitor = mcp.NewElicitor()
	}
	// Servers from a project's .mcp.json wait for approval, asked at the first
	// turn (admitMCP), when there is a frontend to ask; the global file's start
	// now. mcpLatest is the whole config, held or not, for that question and
	// for reloads.
	mcpTrust := &mcp.ProjectTrust{Dir: cwd}
	var mcpLatestMu sync.Mutex
	mcpLatest := mcpCfg
	mcpRun, _ := mcpTrust.Admit(mcpCfg)
	mcpMgr, mcpErrs := mcp.Connect(ctx, mcpRun, elicitor)
	defer mcpMgr.Close()
	for _, e := range mcpErrs {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", e)
	}
	// Persistent memory: one store shared by the Memory tool (agent + sub-agents)
	// and the /memory command.
	staticTools := base.All()
	memStore := memory.New(filepath.Join(cwd, ".klaudia"))
	if opts.restricted {
		memStore = memory.Disabled()
	}
	if memTool, merr := tools.NewMemoryForProject(memStore, cwd); merr == nil && !opts.restricted {
		staticTools = append(staticTools, memTool) // restricted: no tool that only says it is off
	}

	// User-defined skills (~/.klaudia/skills overlaid by .klaudia/skills) become a
	// single Skill tool the model can invoke; the TUI also dispatches /<skill>.
	skills := skill.Load(cwd, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) })
	skillInfos := skillToolInfos(skills)
	if skillTool, serr := tools.NewSkill(skillInfos); serr == nil && skillTool != nil {
		staticTools = append(staticTools, skillTool)
	}

	// buildTools folds the live MCP tools into the fixed local set. It runs at
	// startup and again on every config reload, so the two cannot drift: a tool
	// set assembled one way at boot and another way on reload is how a reloaded
	// session ends up subtly unlike a restarted one.
	//
	// Deferred tool loading: MCP tools can be numerous, so withhold them from
	// the initial request behind a ToolSearch tool that loads them on demand.
	// tools.DeferredBuiltins are withheld the same way.
	buildTools := func() ([]tools.Tool, map[string]bool) {
		all := append([]tools.Tool(nil), staticTools...)
		all = append(all, mcpMgr.Tools(ctx)...)
		if rts, rerr := mcpMgr.ResourceTools(); rerr == nil && len(mcpMgr.Servers()) > 0 {
			all = append(all, rts...)
		}
		deferred := map[string]bool{}
		for _, t := range all {
			if strings.HasPrefix(t.Name(), "mcp__") || tools.DeferredBuiltins[t.Name()] {
				deferred[t.Name()] = true
			}
		}
		if len(deferred) > 0 {
			// Only what is withheld: the rest is already in every request, and
			// a search that answers with Read or Bash wastes the model's turn.
			catalog := make([]tools.ToolInfo, 0, len(deferred))
			for _, t := range all {
				if !deferred[t.Name()] {
					continue
				}
				desc, _ := t.Description(ctx)
				catalog = append(catalog, tools.ToolInfo{Name: t.Name(), Description: desc})
			}
			if ts, terr := tools.NewToolSearch(catalog); terr == nil {
				all = append(all, ts)
			}
		}
		return all, deferred
	}

	baseTools, deferredTools := buildTools()
	base = tools.NewRegistry(baseTools...)

	// deferredTools is replaced when the MCP config reloads, on the watcher's
	// goroutine, while per-turn closures below read it on the session's. Reads
	// go through currentDeferred; the map is swapped whole, never mutated.
	var deferredMu sync.RWMutex
	currentDeferred := func() map[string]bool {
		deferredMu.RLock()
		defer deferredMu.RUnlock()
		return deferredTools
	}

	// Headless has no one to ask. Ordinary work still runs; host changes are
	// refused with the flag that would permit them, so the output says what to
	// do rather than only what failed.
	approver := hostgate.HeadlessApprover(opts.allowHostChanges)
	// The fallback chain: the flag, else config. Resolved once; a model that
	// is also the primary is skipped, since falling back to it changes nothing.
	fallbackNames := opts.fallbackModels
	if len(fallbackNames) == 0 {
		fallbackNames = cfg.FallbackModels
	}
	fallbacks := func(primary anthropic.Model) []anthropic.Model {
		var out []anthropic.Model
		for _, n := range fallbackNames {
			if m := api.ResolveModel(n); m != primary && m != "" {
				out = append(out, m)
			}
		}
		return out
	}
	wiring, err := withAgentTool(base, provider, model, permCtx, approver, opts.maxTurns, deferredTools, cwd, hostGate, cfg.SubagentWorktrees())
	if err != nil {
		return err
	}
	registry := wiring.registry
	// Sub-agents inherit the hooks for the same reason they inherit the host
	// gate: a child must not be the way around a rule the user set.
	wiring.spawner.WithHooks(hooksFor(hookRunner))
	// Effort is inherited unless [subagents] effort says otherwise. Like the
	// sub-agent model, this is a snapshot taken at startup: a later /effort
	// changes the main loop, not children already wired.
	subEffort := effort
	if cfg.Subagents.Effort != "" {
		e, perr := api.ParseEffort(cfg.Subagents.Effort)
		if perr != nil {
			return usageErrorf("[subagents] effort: %s", perr)
		}
		if msg := api.EffortError(string(model), e); msg != "" {
			return usageErrorf("[subagents] effort: %s", msg)
		}
		subEffort = e
	}
	wiring.spawner.WithEffort(api.CapEffort(string(model), subEffort, maxEffort))
	// The child runs the parent's model, so it gets the parent's resolved
	// sizing rather than re-deriving it from the offline table — otherwise a
	// provider-reported cap or a maxTokens override is lost on exactly the
	// workload (a sub-agent writing a file) where it matters.
	wiring.spawner.WithLimits(limits)
	// Sub-agents run the parent's model, so an overloaded provider fails
	// them exactly as it fails the parent; they get the same way out.
	wiring.spawner.WithFallbacks(fallbacks(model))
	// Children spill where the parent does, and know what the parent knows
	// about the environment and the project.
	wiring.spawner.WithSpill(spill)
	wiring.spawner.WithContext(func(dir string) string { return prompt.SubagentContext(dir, cwd) })

	// MCP config hot reload. An edit to any .mcp.json that applies here takes
	// effect in this session instead of at the next start — installing a server
	// and then having to restart to use it is the whole problem.
	//
	// Both registries are rebuilt: the main loop dispatches from `registry`,
	// while sub-agents draw from `base`. Updating one and not the other would
	// give a sub-agent a different tool set than its parent.
	//
	// A config that no longer parses is left alone rather than applied — a
	// half-typed file should not take working servers away mid-session — and
	// that, along with any server that failed to launch, is reported through
	// mcpReloads so a broken edit is not silently indistinguishable from a
	// working one.
	//
	// rebuildMCP is the part a /mcp reconnect needs too: the server may have
	// had no tools to contribute when the registries were last built.
	rebuildMCP := func() {
		next, deferred := buildTools()
		base.Replace(next...)
		registry.Replace(append(append([]tools.Tool(nil), next...), wiring.agentTool)...)
		wiring.spawner.SetDeferred(deferred)
		deferredMu.Lock()
		deferredTools = deferred
		deferredMu.Unlock()
	}
	// admitMCP asks about held project servers at the start of a turn — the
	// first point where a frontend's approver exists — and starts them on a
	// yes, before the loop builds its request, so the turn can use them. It
	// asks once per set per session; Decide returns at once after that.
	latestMCP := func() mcp.Config {
		mcpLatestMu.Lock()
		defer mcpLatestMu.Unlock()
		return mcpLatest
	}
	// startAdmitted applies whatever the trust now admits and rebuilds the
	// registries: after a yes on the first-turn card, and after /mcp's
	// approve row.
	startAdmitted := func() []error {
		run, _ := mcpTrust.Admit(latestMCP())
		errs := mcpMgr.Reload(ctx, run)
		rebuildMCP()
		return errs
	}
	admitMCP := func(ctx context.Context, turn agent.Turn) {
		cfg := latestMCP()
		var confirm mcp.Confirm
		if turn.Approver != nil {
			confirm = func(ctx context.Context, lines, files []string) bool {
				return hostgate.ConfirmMCP(ctx, turn.Approver, lines, files)
			}
		}
		notify := func(msg string) {
			if turn.Emit != nil {
				turn.Emit(agent.Event{Type: agent.EventNotice, Content: "mcp: " + msg})
			}
		}
		ok, notice := mcpTrust.Decide(ctx, cfg, confirm)
		if notice != "" {
			notify(notice)
		}
		if !ok {
			return
		}
		for _, e := range startAdmitted() {
			notify(e.Error())
		}
	}
	mcpReloads := &mcpReloadNotifier{}
	stopWatch, werr := mcp.Watch(cwd, func() {
		cfg, lerr := mcp.LoadConfig(cwd)
		if lerr != nil {
			mcpReloads.emit(mcp.ReloadEvent{ConfigErr: lerr.Error()})
			return
		}
		mcpLatestMu.Lock()
		mcpLatest = cfg
		mcpLatestMu.Unlock()
		// An edited project server is a new set: Admit holds it again, and the
		// next turn asks.
		run, _ := mcpTrust.Admit(cfg)
		errs := mcpMgr.Reload(ctx, run)
		rebuildMCP()
		if len(errs) > 0 {
			msgs := make([]string, 0, len(errs))
			for _, e := range errs {
				msgs = append(msgs, e.Error())
			}
			mcpReloads.emit(mcp.ReloadEvent{ServerErrs: msgs})
		}
	})
	if werr == nil {
		defer stopWatch()
	}
	// The TUI registers its own listener once its model exists and prints into
	// the transcript. Every other mode gets stderr, which is where its other
	// diagnostics already go — and is not stdout, so a notice cannot land in the
	// middle of a stream-json line.
	if !interactive {
		mcpReloads.register(mcpReloadWriter(cmd.ErrOrStderr()))
	}

	// Open the transcript for this session (best effort: a transcript failure
	// should not abort the run).
	//
	// transcript holds the concrete type as well as the interface because
	// agent.Recorder is deliberately just Record — SetSizing is Klaudia's own,
	// and widening the interface would push model sizing into every recorder
	// implementation and into the agent package that has no use for it.
	var recorder agent.Recorder
	var transcript *session.Transcript
	if tr, terr := session.NewTranscript(session.Meta{
		SessionID:      sessionID,
		CWD:            cwd,
		Version:        version.Version,
		GitBranch:      gitBranch(cwd),
		PermissionMode: string(mode),
	}); terr == nil {
		defer func() { _ = tr.Close() }()
		recorder = tr
		transcript = tr
	}

	loop := agent.New(provider, registry)

	// Persist compaction summaries for token-saving resume (a Klaudia divergence).
	onSummary := func(summary string) {
		_ = session.WriteSummary(cwd, sessionID, summary, gitCommit(cwd))
	}

	// One options builder for every frontend.
	//
	// There used to be a separate one per mode, each spelling out its own list
	// of ~15 Options fields, and they had drifted: the stream-json copy was
	// missing Asker, Planner and InitialMessages, so questions, plan approval
	// and --resume were all quietly inert over that transport. The per-turn half
	// now comes from agent.Turn.Apply and the per-mode half from turnSettings,
	// which is small enough to read at a glance and lists only what genuinely
	// differs between modes.
	baseOptions := func(turn agent.Turn, s turnSettings) agent.Options {
		// Stamp the resolved sizing on the transcript before the turn runs.
		// Here rather than at transcript construction because this is the one
		// choke point all five frontends pass through, and because the TUI
		// passes sess.* fresh every turn — so /model is tracked for free
		// instead of needing a hook of its own.
		if transcript != nil {
			transcript.SetSizing(string(s.Model), s.MaxTokens, s.MaxTokensSource, s.ContextWindow, s.ContextSource)
		}
		opts := agent.Options{
			WorkingDir:      cwd,
			Model:           s.Model,
			System:          withExtraDirs(sysPrompt, s.ExtraDirs) + prompt.ModelLine(string(s.Model)),
			MaxTurns:        opts.maxTurns,
			ContextWindow:   s.ContextWindow,
			MaxTokens:       int64(s.MaxTokens),
			MaxTokensSource: s.MaxTokensSource,
			Effort:          s.Effort,
			Permission:      s.Permission,
			Gate:            gateFor(hostGate),
			Hints:           config.Hints,
			DeferredTools:   currentDeferred(),
			Recorder:        recorder,
			WebTools:        true,
			OnSummary:       onSummary,
			Hooks:           hooksFor(hookRunner),
			Plan:            planStore,
			Spill:           spill,
			FallbackModels:  fallbacks(s.Model),
		}
		turn.Apply(&opts)
		return opts
	}

	// Interactive TUI: the default when not headless and not stream-json input.
	// It drives the same loop, prompting the user to resolve permission asks.
	if interactive {
		// Shared settings so slash commands can read/change them between turns.
		sess := &tui.Session{
			// Effective model (flag or config default), never "" — otherwise a
			// non-Anthropic provider would wrongly resolve to the Anthropic default.
			SessionID:      sessionID,
			Model:          modelStr,
			ResolvedModel:  string(model),
			Effort:         effort,
			MaxEffort:      maxEffort,
			Theme:          themeOrWarn(cfg.Theme, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) }), // user default (~/.klaudia) overlaid by project; /theme overrides per session
			PermissionMode: string(mode),
			EnterInserts:   tui.EnterInserts(cfg.Input.Enter, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) }),
			Notify:         tui.ParseNotify(cfg.TUI.Notify, os.Getenv, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) }),
			Hyperlinks:     tui.HyperlinksEnabled(cfg.Links.Enabled, os.Getenv),
			IdleCompact:    cfg.IdleCompact == nil || *cfg.IdleCompact,
			FileLinks:      cfg.Links.File,
			Memory:         memStore,
			MCP: mcpController{mgr: mcpMgr, ctx: ctx, rebuild: rebuildMCP,
				trust: mcpTrust, latest: latestMCP, start: startAdmitted},
			OnMCPReload:         mcpReloads.register,
			Skills:              tuiSkills(skills, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) }),
			Provider:            providerName(cfg),
			SandboxMode:         sandboxMode(cfg.Sandbox),
			CWD:                 cwd,
			GitBranch:           gitBranch(cwd),
			Agents:              tuiAgents(),
			ContextWindow:       limits.ContextWindow,
			ContextWindowSource: limits.ContextSource,
			MaxTokens:           limits.MaxOutputTokens,
			MaxTokensSource:     limits.MaxOutputSource,
			// Nil unless the provider can enumerate its models; /model falls
			// back to type-the-id when it is. Through the cache, so the picker
			// keeps it current.
			ListModels:    cachedModelLister(modelCache, provider),
			ResolveLimits: resolveLimitsFor,
			Trust:         tui.NewTrustController(hostGate),
			Jobs:          jobStore,
			Executor:      executor,
		}
		// Assigned after the literal rather than inside them so the closures
		// can read the session back: /compact and /doctor then use whatever
		// /model and /effort have most recently set, instead of the values
		// captured at startup — which is what /compact did when it closed over
		// modelStr.
		sess.Doctor = func() string {
			// The session's own figures, not the startup ones: /model records
			// the chosen model's sizing there, so /doctor after a switch
			// reports the model it switched to.
			// The table-disagreement figures come from the resolver: the
			// session keeps only the answer, not what it overrode.
			live := resolveLimitsFor(sess.Model, api.ModelInfo{})
			live.MaxOutputTokens, live.MaxOutputSource = sess.MaxTokens, sess.MaxTokensSource
			live.ContextWindow, live.ContextSource = sess.ContextWindow, sess.ContextWindowSource
			in := buildDoctorInput(cfg, api.ResolveModel(sess.Model), live, sess.Effort, cwd, mcpCfg, hookRunner)
			in.Egress = egressDoctor(cfg.Sandbox, executor, egressProxy)
			return doctor.Format(doctor.Run(in))
		}
		// 1024 is plenty for a subject and a few lines; Complete refuses a
		// reply that hits it rather than committing half a message.
		if hookRunner.Has(hooks.Notification) {
			// No one to confirm with here: a project's Notification hooks run
			// only once the project's hooks are approved, and are otherwise
			// skipped with a notice, as the runner does for any event.
			sess.OnNotify = func(reason, message string) {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				hookRunner.Run(ctx, hooks.Input{Event: hooks.Notification, Message: message, NotificationType: reason}, nil)
			}
		}
		sess.OnRewind = func(keep int) {
			if transcript != nil {
				_ = transcript.RecordRewind(keep)
			}
		}
		sess.Suggest = func(ctx context.Context, prompt string) (string, error) {
			return loop.Complete(ctx, prompt, api.ResolveModel(sess.Model), 1024, sess.Effort)
		}
		sess.Compact = func(ctx context.Context, history []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
			return compactAndPersist(ctx, history, func(ctx context.Context, history []anthropic.BetaMessageParam) ([]anthropic.BetaMessageParam, string, error) {
				return loop.Compact(ctx, history, api.ResolveModel(sess.Model), int64(sess.MaxTokens), sess.Effort)
			}, func(summary string) {
				onSummary(summary)
				// /compact runs outside the loop, so autocompact's boundary
				// write never sees it; the TUI is one session, so the process
				// transcript is the right one.
				if transcript != nil {
					_ = transcript.RecordCompaction(summary)
				}
			})
		}
		sess.SummariseBefore = func(ctx context.Context, head []anthropic.BetaMessageParam, kept int) ([]anthropic.BetaMessageParam, string, error) {
			newHead, summary, err := loop.Compact(ctx, head, api.ResolveModel(sess.Model), int64(sess.MaxTokens), sess.Effort)
			// Not onSummary: the .summary.md file is the resume fallback for a
			// summary of the whole conversation, which this is not.
			if err == nil && transcript != nil {
				_ = transcript.RecordPartialCompaction(summary, kept)
			}
			return newHead, summary, err
		}
		extraDirs = func() []string { return sess.ExtraDirs }
		runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
			// An MCP server's elicitation has to reach the same prompt the
			// model's own questions use, and the Asker that reaches it is
			// handed over per turn. Point the elicitor at this turn's.
			elicitor.SetAsker(turn.Asker)
			admitMCP(ctx, turn)
			return loop.Run(ctx, baseOptions(turn, turnSettings{
				Model: api.ResolveModel(sess.Model), // resolved fresh each turn
				// Permission is not set here: the mode comes with the Turn
				// (tui.Model.liveMode), so a /mode bypass or an approved
				// ExitPlanMode takes effect on the very next tool dispatch
				// inside the running turn rather than at the next TUI turn
				// boundary.
				ExtraDirs: sess.ExtraDirs,
				Effort:    sess.Effort, // resolved fresh each turn, like Model
				// Live too: /model records the new model's sizing, so a switch
				// to a model with a different output cap takes effect on the
				// next turn rather than keeping the startup model's.
				MaxTokens:       sess.MaxTokens,
				ContextWindow:   sess.ContextWindow,
				MaxTokensSource: sess.MaxTokensSource,
				ContextSource:   sess.ContextWindowSource,
			}), turn.Emit)
		}
		return tui.Run(ctx, tui.RunFunc(runFn), initialMessages, sess)
	}

	// Stream-json input: drive a persistent agent over stdin/stdout (the
	// embedding channel). Each user message is a turn; permission asks,
	// questions and plan approvals are surfaced as control_request and answered
	// by the peer.
	if opts.inputFormat == "stream-json" {
		driver := streamjson.NewDriver(cmd.OutOrStdout())
		driver.Interject = jobInbox.Interject
		// Background sub-agents: results queue for the model's next turn (or
		// the running one), and the peer hears about each as it finishes.
		driver.Background = agent.NewBackground(ctx, func(t agent.BackgroundTask) {
			jobInbox.Add(t.Notice())
			status := "completed"
			if t.Err != nil {
				status = "failed"
			}
			driver.Notify("agent_done", map[string]any{
				"agent_id": t.ID, "label": t.Label, "status": status,
				"note": "delivered to the model with its next turn, or mid-turn if one is running",
			})
		})
		jobExitHook = func(st tools.JobStatus) {
			driver.Notify("job_exit", map[string]any{
				"job_id": st.ID, "name": st.Name, "exit_code": st.ExitCode,
				"note": "delivered to the model with its next turn, or mid-turn if one is running",
			})
		}
		jobMatchHook = func(st tools.JobStatus, n int) {
			driver.Notify("job_output", map[string]any{
				"job_id": st.ID, "name": st.Name, "watch": st.Watch, "lines": n,
			})
		}
		driver.Init = map[string]any{
			"session_id": sessionID, "cwd": cwd, "model": string(model),
			"permissionMode": string(mode), "tools": registry.Names(),
		}
		runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
			elicitor.SetAsker(turn.Asker)
			admitMCP(ctx, turn)
			return loop.Run(ctx, baseOptions(turn, turnSettings{
				Model:           model,
				Permission:      permCtx,
				Effort:          effort,
				MaxTokens:       limits.MaxOutputTokens,
				ContextWindow:   limits.ContextWindow,
				MaxTokensSource: limits.MaxOutputSource,
				ContextSource:   limits.ContextSource,
			}), turn.Emit)
		}
		// initialMessages, not nil: an explicit --resume/--continue was being
		// resolved and then dropped on this path, so the agent started a fresh
		// conversation while the CLI reported it had resumed one.
		return driver.Run(ctx, cmd.InOrStdin(), initialMessages, runFn)
	}

	// ACP input: serve the Agent Client Protocol over stdio, so an editor
	// drives Klaudia with its own UI. Same loop, same tools; only the wire
	// format and the permission plumbing differ from stream-json.
	if opts.inputFormat == "acp" {
		runFn := func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
			elicitor.SetAsker(turn.Asker)
			admitMCP(ctx, turn)
			return loop.Run(ctx, baseOptions(turn, turnSettings{
				// Permission comes from the Turn: an ACP client keeps a mode
				// per editor session, so one resolved here would be wrong for
				// every session but the first.
				Model:           model,
				Effort:          effort,
				MaxTokens:       limits.MaxOutputTokens,
				ContextWindow:   limits.ContextWindow,
				MaxTokensSource: limits.MaxOutputSource,
				ContextSource:   limits.ContextSource,
			}), turn.Emit)
		}
		srv := acp.New(acp.Options{
			Run:  runFn,
			CWD:  cwd,
			Mode: mode,
			// The editor's first thread *is* the session the CLI resolved, so
			// --resume/--continue reaches ACP: same id, same transcript, same
			// seeded history.
			SessionID: sessionID,
			History:   initialMessages,
			// Per session, not the process-wide recorder baseOptions installs.
			// An editor opens several threads and each is its own conversation;
			// one transcript for all of them interleaves them on disk. Turn.
			// Recorder overrides the process one for every ACP turn, so the
			// outer transcript stays unused — and because a Writer creates its
			// file on the first append, an unused one leaves nothing behind.
			Transcript: acpTranscript(cwd, mode, string(model), limits),
			// Keyed by the ACP session id, not the process's: the process-wide
			// planStore is the one plan per process, and ACP runs many.
			Plan: func(id string) *plan.Store {
				return plan.New(plan.PathFor(session.ConfigRoot(), id))
			},
			LoadHistory:   acpLoadHistory(cwd),
			ListSessions:  func() []acp.SessionSummary { return acpSessions(cwd) },
			Commands:      acpCommands(skills),
			ContextWindow: limits.ContextWindow,
			Log:           func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "acp:", m) },
		}, cmd.OutOrStdout())
		return srv.Serve(ctx, cmd.InOrStdin())
	}

	// Autonomous goal loop: iterate against the spec until complete or capped.
	if opts.loop {
		return runGoalLoop(ctx, cmd, loopRun{
			run: func(ctx context.Context, turn agent.Turn) (agent.Result, error) {
				admitMCP(ctx, turn)
				return loop.Run(ctx, baseOptions(turn, turnSettings{
					Model:           model,
					Permission:      permCtx,
					Effort:          effort,
					MaxTokens:       limits.MaxOutputTokens,
					ContextWindow:   limits.ContextWindow,
					MaxTokensSource: limits.MaxOutputSource,
					ContextSource:   limits.ContextSource,
				}), turn.Emit)
			},
			approver:   approver,
			interject:  jobInbox.Interject,
			cwd:        cwd,
			mode:       mode,
			iterations: opts.maxIterations,
			render:     r,
		})
	}

	// Single-shot headless run. For stream-json output, emit JS-compatible
	// message envelopes (assistant/user) via an envelope recorder alongside the
	// transcript; the simplified delta events are not used in that mode.
	runRecorder := agent.Recorder(recorder)
	notices := noticeWriter(cmd.ErrOrStderr())
	// Before the init line, so its tool list includes servers approved here.
	admitMCP(ctx, agent.Turn{Approver: approver, Emit: notices})
	emit := func(ev agent.Event) {
		notices(ev)
		_ = r.Event(ev)
	}
	var partial func(anthropic.BetaRawMessageStreamEventUnion)
	if format == FormatStreamJSON {
		// Serialize envelope + partial writes to the same stream.
		var writeMu sync.Mutex
		out := cmd.OutOrStdout()
		runRecorder = multiRecorder{recorder, newEnvelopeRecorder(out, sessionID)}
		// Claude Code opens its stream-json output with a system/init line,
		// and consumers written against it read the session's model, tools
		// and mode from there before the first message.
		if line, err := json.Marshal(map[string]any{
			"type": "system", "subtype": "init", "session_id": sessionID, "cwd": cwd,
			"model": string(model), "permissionMode": string(mode), "tools": registry.Names(),
			"uuid": uuid.NewString(),
		}); err == nil {
			fmt.Fprintln(out, string(line))
		}
		// The envelope recorder emits the conversation, so the renderer is not
		// asked to; notices still go to stderr, where they do not collide with
		// the JSON stream.
		emit = notices
		if opts.partialMessages {
			partial = newPartialEmitter(out, sessionID, &writeMu).emit
		}
	}
	// Background sub-agents in a one-shot run: their results join the job
	// inbox, so they reach the model mid-run, and the run does not end while
	// one is still working (see the follow-up loop after Run). Without this a
	// headless run kept every sub-agent in the foreground.
	bgDone := make(chan struct{}, 1)
	headlessBG := agent.NewBackground(ctx, func(t agent.BackgroundTask) {
		jobInbox.Add(t.Notice())
		select {
		case bgDone <- struct{}{}:
		default:
		}
	})
	headlessOpts := baseOptions(agent.Turn{
		Prompt:     opts.prompt,
		History:    initialMessages,
		Emit:       emit,
		Approver:   approver,
		Interject:  jobInbox.Interject,
		Background: headlessBG,
	}, turnSettings{
		Model:           model,
		Permission:      permCtx,
		Effort:          effort,
		MaxTokens:       limits.MaxOutputTokens,
		ContextWindow:   limits.ContextWindow,
		MaxTokensSource: limits.MaxOutputSource,
		ContextSource:   limits.ContextSource,
	})
	// Three fields the shared builder has no business knowing about: a headless
	// run uses the envelope recorder rather than the plain transcript, it is the
	// only mode that streams raw model events, and its deferred set is fixed
	// before the run because there is no turn boundary for a reload to land on.
	headlessOpts.Recorder = runRecorder
	headlessOpts.PartialMessages = partial
	headlessOpts.DeferredTools = currentDeferred()
	res, err := loop.Run(ctx, headlessOpts, emit)
	// The model may finish its turn while background agents it started are
	// still working: wait for them, and give their results (and anything
	// else queued meanwhile) to the model as a turn of its own, until nothing
	// is outstanding. The result reports the final answer and the totals.
	// Only when the run used background agents: an ordinary -p run is not
	// extended because a job happened to exit near its end.
	for err == nil && len(headlessBG.List()) > 0 {
		if headlessBG.Running() > 0 {
			select {
			case <-bgDone:
			case <-ctx.Done():
				err = ctx.Err()
				continue
			}
		}
		in := jobInbox.Interject()
		if in.Empty() {
			if headlessBG.Running() == 0 {
				break
			}
			continue
		}
		prev := res
		headlessOpts.Prompt, headlessOpts.InitialMessages = in.Text, res.Messages
		res, err = loop.Run(ctx, headlessOpts, emit)
		res.NumTurns += prev.NumTurns
		res.InputTokens += prev.InputTokens
		res.OutputTokens += prev.OutputTokens
		res.CacheReadInputTokens += prev.CacheReadInputTokens
		res.CacheCreationInputTokens += prev.CacheCreationInputTokens
	}

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
		out.Result = "Error: " + config.Hints.FriendlyError(err)
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
func hostChangeWasBlocked(g *hostgate.HostGate) bool {
	for _, r := range g.Reports() {
		if r.Enforced {
			return true
		}
	}
	return false
}

// ExecuteContext runs the root command against ctx. Cancelling ctx (SIGINT /
// SIGTERM from main) unwinds the run, which tears down background jobs,
// browsers, MCP servers and the transcript through the existing defers.
func ExecuteContext(ctx context.Context) int {
	stopTracing := setupTracing(ctx, os.Getenv)
	err := NewRootCommand().ExecuteContext(ctx)
	stopTracing()
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

// subagentTypes resolves a subagent_type for the spawner from Klaudia's
// built-in types.
func subagentTypes(name string) (agent.SubagentType, bool) {
	t, ok := subagent.Lookup(name)
	if !ok {
		return agent.SubagentType{}, false
	}
	return agent.SubagentType{SystemPrompt: t.SystemPrompt, Mode: t.Mode, Tools: t.Filter}, true
}

// gateFor is Klaudia's host gate as the loop's agent.Gate: nil for a nil gate,
// never an interface holding a nil pointer.
func gateFor(g *hostgate.HostGate) agent.Gate {
	if g == nil {
		return nil
	}
	return g
}
