package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/greenthread/klaudia/internal/agent"
	"github.com/greenthread/klaudia/internal/api"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/tools"
	"github.com/greenthread/klaudia/internal/version"
)

// defaultSystemPrompt is the minimal Klaudia/Claude Code identity prompt sent
// when no override is given. The Claude Code identity is required on the OAuth
// auth path. The full system-prompt assembly lands in a later phase.
const defaultSystemPrompt = "You are an interactive agent that helps users with software engineering tasks. Use the instructions below and the tools available to you to assist the user."

// options holds parsed CLI flags, mirroring the JS commander surface
// (08-entry.js setupCommander). Only the Phase 0 subset is wired so far.
type options struct {
	print          bool
	prompt         string
	model          string
	outputFormat   string
	permissionMode string
	dangerouslySkip bool
	verbose        bool
	maxTurns       int
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
	f.StringVar(&opts.permissionMode, "permission-mode", "default", "Permission mode: default|acceptEdits|bypassPermissions|plan|dontAsk")
	f.BoolVar(&opts.dangerouslySkip, "dangerously-skip-permissions", false, "Skip all permission checks (sets bypassPermissions)")
	f.BoolVar(&opts.verbose, "verbose", false, "Verbose output (required for stream-json)")
	f.IntVar(&opts.maxTurns, "max-turns", 0, "Limit the number of agentic loop turns (0 = unlimited)")

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

	if !opts.print {
		return fmt.Errorf("interactive (TUI) mode is not implemented yet; use -p \"<prompt>\" for headless mode")
	}

	if format == FormatStreamJSON && !opts.verbose {
		return fmt.Errorf("--output-format stream-json requires --verbose")
	}

	start := time.Now()
	ctx := cmd.Context()
	r := NewRenderer(format, cmd.OutOrStdout())
	sessionID := uuid.NewString()

	// Resolve auth and build the API client.
	cred, err := api.ResolveCredential()
	if err != nil {
		return err
	}
	client := api.New(cred, os.Getenv("KLAUDIA_CUSTOM_ENDPOINT"))

	// Build the tool registry.
	registry, err := tools.DefaultRegistry()
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

	// Run the agentic loop.
	loop := agent.New(client, registry)
	emit := func(ev agent.Event) { _ = r.Event(ev) }
	res, err := loop.Run(ctx, agent.Options{
		Prompt:      opts.prompt,
		Model:       api.ResolveModel(opts.model),
		System:      defaultSystemPrompt,
		MaxTurns:    opts.maxTurns,
		Permission:  permission.Context{Mode: mode},
		Interactive: false, // headless mode
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
		out.Result = fmt.Sprintf("Error: %v", err)
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
