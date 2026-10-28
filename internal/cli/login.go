package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/greenthread-ai/klaudia/internal/anthropicauth"
)

// login is --login: store an Anthropic API key in Klaudia's credential store
// (credentials.json under the config root, 0600), which ResolveCredential
// reads after the environment and before a borrowed Claude Code session.
//
// A flag, not a `klaudia login` subcommand: a positional argument is a
// prompt, and a subcommand would change how "klaudia login to the server and
// fix the auth" is read. The key is prompted for without echo, or read from
// stdin when piped; there is deliberately no --api-key flag, since a key on
// the command line lands in shell history and the process list. (From PR #100.)
func login(cmd *cobra.Command) error {
	key, err := readAPIKey(cmd)
	if err != nil {
		return err
	}
	path, err := anthropicauth.SaveLoginKey(key)
	if err != nil {
		return fmt.Errorf("store credentials: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Saved the API key to %s\n", path)
	return nil
}

// readAPIKey prompts without echo on a terminal, and otherwise reads one
// line from stdin, so a script can pipe the key in.
func readAPIKey(cmd *cobra.Command) (string, error) {
	if f, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(f.Fd()) {
		fmt.Fprint(cmd.OutOrStdout(), "Anthropic API key: ")
		b, err := term.ReadPassword(f.Fd())
		fmt.Fprintln(cmd.OutOrStdout())
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if line = strings.TrimSpace(line); line == "" && err != nil {
		return "", fmt.Errorf("no API key on stdin")
	}
	return line, nil
}
