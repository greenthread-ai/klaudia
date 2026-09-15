package tui

import (
	"fmt"
	"strings"
)

// mcpReloadMsg carries the outcome of an MCP config hot reload from the config
// watcher's goroutine into the event loop.
type mcpReloadMsg struct{ event MCPReloadEvent }

// maxReloadErrsShown bounds the per-server failures listed in one notice. A
// config that names a dozen broken servers has one problem, not a dozen, and
// the point of the line is to send you to /mcp rather than to reproduce it.
const maxReloadErrsShown = 3

// onMCPReload reports a hot reload that did not fully work.
//
// A successful reload stays silent: it is the common case, and announcing it
// would mean every save of an unrelated key in .mcp.json printed a line. A
// failed one has to speak, because the alternative — what happened before this
// existed — is that a typo in the config is indistinguishable from a clean
// reload until a tool call fails much later with an unrelated-looking error.
func (m *Model) onMCPReload(ev MCPReloadEvent) {
	if !ev.Failed() {
		return
	}
	if ev.ConfigErr != "" {
		// Say that the old servers survived. Without it the natural reading is
		// that MCP is now down, and the useful fact is the opposite: the
		// session is still working off the last config that parsed.
		m.appendLine(errStyle.Render(
			"MCP config not reloaded: " + ev.ConfigErr + "\nThe previously loaded servers are still running."))
		return
	}

	shown := ev.ServerErrs
	extra := 0
	if len(shown) > maxReloadErrsShown {
		extra = len(shown) - maxReloadErrsShown
		shown = shown[:maxReloadErrsShown]
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("MCP reload: %d server(s) failed to start", len(ev.ServerErrs)))
	for _, e := range shown {
		b.WriteString("\n  " + e)
	}
	if extra > 0 {
		b.WriteString(fmt.Sprintf("\n  …and %d more", extra))
	}
	b.WriteString("\n/mcp shows the current state; /mcp reconnect <name> retries one.")
	m.appendLine(errStyle.Render(b.String()))
}
