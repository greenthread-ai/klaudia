// Command klaudia is the single-binary Go port of the Klaudia agentic coding
// tool. It dispatches to headless (-p) or interactive (TUI) mode.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/greenthread-ai/klaudia/internal/cli"
)

func main() {
	// Klaudia's children now run in their own process groups, so a Ctrl+C at
	// the terminal reaches Klaudia and nothing else. That is deliberate — a
	// stray signal should not kill a managed dev server — but it makes this
	// handler load-bearing: without it, dying on SIGINT would leave every
	// background job orphaned. Cancelling the root context runs the same
	// group-kill path as stopping a job by hand.
	//
	// The second signal is not caught. If cleanup itself wedges, the user's
	// next Ctrl+C must still work.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.ExecuteContext(ctx))
}
