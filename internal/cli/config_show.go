package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/greenthread-ai/klaudia/internal/config"
)

// newConfigCommand builds `klaudia config` and its `show` subcommand.
func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "config",
		Short:         "Inspect Klaudia configuration",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		// With no subcommand, print help rather than doing nothing.
		RunE: func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newConfigShowCommand())
	return cmd
}

// newConfigShowCommand builds `klaudia config show [--origin]`: print the
// effective merged configuration as `key = value` lines. The resolved API key
// is never printed — the apiKey line reports only whether a key is set and
// where it came from. With --origin, each line is annotated with the layer that
// set it (default / home / project / env).
func newConfigShowCommand() *cobra.Command {
	var withOrigin bool
	cmd := &cobra.Command{
		Use:           "show",
		Short:         "Print the effective merged config (secrets redacted; --origin annotates each value's source)",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			settings := config.Origins(cwd)
			out := cmd.OutOrStdout()
			// Width for aligning the origin comments so the column reads.
			width := 0
			if withOrigin {
				for _, s := range settings {
					if n := len(fmt.Sprintf("%s = %s", s.Key, quoteValue(s.Value))); n > width {
						width = n
					}
				}
			}
			for _, s := range settings {
				line := fmt.Sprintf("%s = %s", s.Key, quoteValue(s.Value))
				if withOrigin {
					fmt.Fprintf(out, "%-*s  # %s\n", width, line, s.Origin)
				} else {
					fmt.Fprintln(out, line)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&withOrigin, "origin", false, "Annotate each value with its source (default/home/project/env)")
	return cmd
}

// quoteValue renders a setting value for a `key = value` line: bare for the
// forms that are not literal strings (numbers, booleans, list brackets, and the
// redaction notes, which are parenthesised), quoted otherwise so a plain string
// reads as one.
func quoteValue(v string) string {
	if v == "" {
		return `""`
	}
	switch v[0] {
	case '[', '(', '$':
		return v
	}
	switch v {
	case "true", "false":
		return v
	}
	// Numbers pass through unquoted.
	if isNumeric(v) {
		return v
	}
	return fmt.Sprintf("%q", v)
}

func isNumeric(v string) bool {
	dot := false
	for i, r := range v {
		switch {
		case r >= '0' && r <= '9':
		case r == '-' && i == 0:
		case r == '.' && !dot:
			dot = true
		default:
			return false
		}
	}
	return v != "" && v != "-" && v != "."
}
