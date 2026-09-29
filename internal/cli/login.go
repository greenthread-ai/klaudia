package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/greenthread-ai/klaudia/internal/api"
)

// newLoginCommand builds `klaudia login`, which stores an Anthropic API key in
// ~/.klaudia/credentials.json (0600) for ResolveCredential to read.
//
// A full interactive OAuth device flow (signing in with a Claude subscription)
// is intentionally out of scope here; Klaudia still borrows an existing Claude
// Code OAuth session automatically (macOS Keychain, or ~/.claude/.credentials.json
// on Linux). This command's job is the API-key case.
func newLoginCommand() *cobra.Command {
	var apiKey string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Store an Anthropic API key for Klaudia to use",
		Long: `Store an Anthropic API key so Klaudia can authenticate without
ANTHROPIC_API_KEY set in every shell.

The key is written to ~/.klaudia/credentials.json (0600, honouring
KLAUDIA_CONFIG_DIR) and read as a fallback after ANTHROPIC_API_KEY /
ANTHROPIC_AUTH_TOKEN but before any borrowed Claude Code session.

  klaudia login                 # prompt for the key (no echo on a terminal)
  klaudia login --api-key sk-…  # non-interactive, for scripts

OAuth device-flow login (a Claude subscription sign-in) is not yet implemented;
Klaudia reuses an existing Claude Code OAuth session when one is present.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			key := strings.TrimSpace(apiKey)
			if key == "" {
				var err error
				if key, err = promptAPIKey(cmd); err != nil {
					return err
				}
			}
			if key == "" {
				return fmt.Errorf("no API key provided")
			}
			path, err := api.WriteLoginAPIKey(key)
			if err != nil {
				return fmt.Errorf("store credentials: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved API key to %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key to store (non-interactive; omit to be prompted)")
	return cmd
}

// promptAPIKey reads an API key from the command's input. When stdin is a
// terminal the key is read without echo; otherwise (piped input) it reads a
// single line, so scripts can feed the key on stdin as well as via --api-key.
func promptAPIKey(cmd *cobra.Command) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), "Anthropic API key: ")
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.OutOrStdout())
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" && err != nil {
		return "", err
	}
	return line, nil
}
