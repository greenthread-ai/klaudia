package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/greenthread/klaudia/internal/version"
)

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

	start := time.Now()
	r := NewRenderer(format, cmd.OutOrStdout())

	// TODO(phase1): replace this stub with the real agent loop (api + agent pkgs).
	res := ResultMessage{
		Type:         "result",
		Subtype:      "success",
		IsError:      false,
		DurationMS:   time.Since(start).Milliseconds(),
		NumTurns:     0,
		Result:       fmt.Sprintf("[phase0 stub] received prompt: %q", opts.prompt),
		StopReason:   "end_turn",
		SessionID:    uuid.NewString(),
		TotalCostUSD: 0,
		UUID:         uuid.NewString(),
	}
	return r.Result(res)
}

// Execute runs the root command, returning the process exit code.
func Execute() int {
	if err := NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}
