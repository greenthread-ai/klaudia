package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/config"
	"github.com/greenthread/klaudia/internal/mcp"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/prompt"
	"github.com/greenthread/klaudia/internal/sandbox"
	"github.com/greenthread/klaudia/internal/session"
	"github.com/greenthread/klaudia/internal/streamjson"
	"github.com/greenthread/klaudia/internal/subagent"
	"github.com/greenthread/klaudia/internal/tools"
	"github.com/greenthread/klaudia/internal/tui"
	"github.com/greenthread/klaudia/internal/version"
)

// withAgentTool returns a registry that is the base tools plus the Agent tool,
// wired to a sub-agent spawner that draws from the base tools.
func withAgentTool(base *tools.Registry, provider api.Provider, model anthropic.Model, perm permission.Context, approver agent.Approver, maxTurns int) (*tools.Registry, error) {
	spawner := agent.NewSpawner(provider, base, model, perm, approver, maxTurns)

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

// buildProvider selects and constructs the model provider from config. It
// returns the provider and the provider's default model. Anthropic is the
// default; "openai" uses an OpenAI-compatible Chat Completions endpoint.
func buildProvider(cfg config.Config) (api.Provider, string, error) {
	switch cfg.Provider {
	case config.ProviderOpenAI:
		if cfg.BaseURL == "" {
			return nil, "", fmt.Errorf(".klaudia/config.json: provider \"openai\" requires baseURL")
		}
		key := cfg.ResolveAPIKey()
		if key == "" {
			return nil, "", fmt.Errorf(".klaudia/config.json: provider \"openai\" needs apiKey or apiKeyEnv")
		}
		return api.NewOpenAIProvider(cfg.BaseURL, key), cfg.Model, nil
	default:
		cred, err := api.ResolveCredential()
		if err != nil {
			return nil, "", err
		}
		return api.New(cred, os.Getenv("KLAUDIA_CUSTOM_ENDPOINT")), cfg.Model, nil
	}
}

// buildExecutor selects the Bash execution backend from config. Container mode
// degrades gracefully to the local executor when misconfigured or when the
// runtime isn't installed (warn explains why).
func buildExecutor(sb config.Sandbox, warn func(string)) sandbox.Executor {
	if sb.Mode != config.SandboxContainer {
		return sandbox.NewLocal()
	}
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


// options holds parsed CLI flags, mirroring the JS commander surface
// (08-entry.js setupCommander). Only the Phase 0 subset is wired so far.
type options struct {
	print           bool
	prompt          string
	model           string
	outputFormat    string
	inputFormat     string
	permissionMode  string
	dangerouslySkip bool
	verbose         bool
	maxTurns        int
	resume          string // --resume <session-id>
	continueSession bool   // --continue
	forkSession     bool   // --fork-session
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

	f := cmd.Flags()
	f.BoolVarP(&opts.print, "print", "p", false, "Non-interactive mode: print result to stdout and exit")
	f.StringVar(&opts.model, "model", "", "Model alias (haiku|sonnet|opus) or full model ID")
	f.StringVar(&opts.outputFormat, "output-format", "text", "Output format: text|json|stream-json")
	f.StringVar(&opts.inputFormat, "input-format", "text", "Input format: text|stream-json (stream-json drives a persistent agent over stdin)")
	f.StringVar(&opts.permissionMode, "permission-mode", "default", "Permission mode: default|acceptEdits|bypassPermissions|plan|dontAsk")
	f.BoolVar(&opts.dangerouslySkip, "dangerously-skip-permissions", false, "Skip all permission checks (sets bypassPermissions)")
	f.BoolVar(&opts.verbose, "verbose", false, "Verbose output (required for stream-json)")
	f.IntVar(&opts.maxTurns, "max-turns", 0, "Limit the number of agentic loop turns (0 = unlimited)")
	f.StringVarP(&opts.resume, "resume", "r", "", "Resume a session by ID")
	f.BoolVar(&opts.continueSession, "continue", false, "Resume the most recent session in this directory")
	f.BoolVar(&opts.forkSession, "fork-session", false, "When resuming, start a new session ID (preserves the original)")

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

	// Mode: stream-json input (embedding) | headless -p | interactive TUI.
	interactive := !opts.print && opts.inputFormat != "stream-json"
	if opts.print && format == FormatStreamJSON && !opts.verbose {
		return fmt.Errorf("--output-format stream-json requires --verbose")
	}

	start := time.Now()
	ctx := cmd.Context()
	r := NewRenderer(format, cmd.OutOrStdout())
	cwd, _ := os.Getwd()

	// Resolve the session: resume by id, continue the most recent, or start new.
	// --fork-session writes to a fresh id while preserving the original.
	var initialMessages []anthropic.BetaMessageParam
	resumeID := opts.resume
	if opts.continueSession && resumeID == "" {
		if id, ok := session.MostRecent(cwd); ok {
			resumeID = id
		} else {
			return fmt.Errorf("--continue: no previous session found in this directory")
		}
	}
	sessionID := uuid.NewString()
	if resumeID != "" {
		entries, rerr := session.Read(session.Path(cwd, resumeID))
		if rerr != nil {
			return fmt.Errorf("resume %s: %w", resumeID, rerr)
		}
		initialMessages, rerr = agent.MessagesFromEntries(entries)
		if rerr != nil {
			return fmt.Errorf("resume %s: %w", resumeID, rerr)
		}
		if !opts.forkSession {
			sessionID = resumeID // continue appending to the same transcript
		}
	}

	// Select the model provider (.klaudia/config.json: anthropic | openai).
	cfg := config.Load(cwd)
	provider, providerModel, err := buildProvider(cfg)
	if err != nil {
		return err
	}

	// Resolve the permission mode (--dangerously-skip-permissions wins).
	mode := permission.Mode(opts.permissionMode)
	if opts.dangerouslySkip {
		mode = permission.ModeBypassPermissions
	}
	if !mode.Valid() {
		return fmt.Errorf("invalid permission mode %q", opts.permissionMode)
	}
	// --model overrides the config/provider default.
	modelStr := opts.model
	if modelStr == "" {
		modelStr = providerModel
	}
	model := api.ResolveModel(modelStr)
	permCtx := permission.Context{Mode: mode}
	// Assemble the full system prompt (base instructions + env context +
	// CLAUDE.md) once for this run.
	sysPrompt := prompt.System(cwd, string(model))

	// Build the tool registry. Sub-agents draw from the base tools (incl. any
	// MCP tools); the top-level registry adds the Agent tool.
	executor := buildExecutor(cfg.Sandbox, func(m string) { fmt.Fprintln(cmd.ErrOrStderr(), "warning:", m) })
	base, err := tools.DefaultRegistry(executor)
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
	base = tools.NewRegistry(baseTools...)

	// Headless has no interactive approver, so permission "ask" denies.
	approver := agent.DenyAll
	registry, err := withAgentTool(base, provider, model, permCtx, approver, opts.maxTurns)
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
		defer tr.Close()
		recorder = tr
	}

	loop := agent.New(provider, registry)

	// Interactive TUI: the default when not headless and not stream-json input.
	// It drives the same loop, prompting the user to resolve permission asks.
	if interactive {
		// Shared settings so /model can change the model between turns.
		sess := &tui.Session{Model: opts.model, PermissionMode: string(mode)}
		runFn := func(ctx context.Context, prompt string, history []anthropic.BetaMessageParam, ap agent.Approver, emit agent.Emitter) (agent.Result, error) {
			return loop.Run(ctx, agent.Options{
				Prompt:          prompt,
				Model:           api.ResolveModel(sess.Model), // resolved fresh each turn
				System:          sysPrompt,
				MaxTurns:        opts.maxTurns,
				Permission:      permCtx,
				Approver:        ap,
				InitialMessages: history,
				Recorder:        recorder,
				WebTools:        true,
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
				Prompt:          prompt,
				Model:           model,
				System:          sysPrompt,
				MaxTurns:        opts.maxTurns,
				Permission:      permCtx,
				Approver:        ap,
				InitialMessages: history,
				Recorder:        recorder,
				WebTools:        true,
			}, emit)
		}
		return driver.Run(ctx, cmd.InOrStdin(), runFn)
	}

	// Single-shot headless run.
	emit := func(ev agent.Event) { _ = r.Event(ev) }
	res, err := loop.Run(ctx, agent.Options{
		Prompt:          opts.prompt,
		Model:           model,
		System:          sysPrompt,
		MaxTurns:        opts.maxTurns,
		Permission:      permCtx,
		Approver:        approver,
		InitialMessages: initialMessages,
		Recorder:        recorder,
		WebTools:        true,
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
			"input_tokens":  res.InputTokens,
			"output_tokens": res.OutputTokens,
		},
		UUID: uuid.NewString(),
	}
	if err != nil {
		out.Subtype = "error_during_execution"
		out.Result = "Error: " + api.FriendlyError(err)
	}
	if rerr := r.Result(out); rerr != nil {
		return rerr
	}
	if err != nil {
		// The error is already rendered into the result payload; signal a
		// non-zero exit without printing it again to stderr.
		return errRendered
	}
	return nil
}

// errRendered marks a run error that has already been emitted in the result
// output, so Execute exits non-zero without re-printing it.
var errRendered = fmt.Errorf("run failed")

// Execute runs the root command, returning the process exit code.
func Execute() int {
	if err := NewRootCommand().Execute(); err != nil {
		if err != errRendered {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		return 1
	}
	return 0
}
