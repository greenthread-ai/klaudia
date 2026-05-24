// Command klaudia is the single-binary Go port of the Klaudia agentic coding
// tool. It dispatches to headless (-p) or interactive (TUI) mode.
package main

import (
	"os"

	"github.com/greenthread/klaudia/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
