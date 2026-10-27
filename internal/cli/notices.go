package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/mcp"
	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// noticeWriter surfaces notice events on stderr during a headless run.
//
// The text and json formats deliberately suppress intermediate events: stdout
// is the result and nothing else, because `klaudia -p` gets piped into things.
// But a notice is not a step in the work, it is a diagnostic the operator is
// the only person who can act on — a hook that failed to start, project hooks
// waiting on an approval nobody is there to give, an MCP server that could not
// be reloaded. Those were being emitted into a renderer that dropped them,
// which is indistinguishable from not reporting them.
//
// stderr rather than stdout, so the output contract is unchanged and a notice
// cannot land in the middle of a stream-json line.
func noticeWriter(w io.Writer) func(agent.Event) {
	return func(ev agent.Event) {
		if ev.Type != "notice" || strings.TrimSpace(ev.Content) == "" {
			return
		}
		fmt.Fprintln(w, ev.Content)
	}
}

// mcpReloadWriter reports a failed MCP hot reload on stderr.
//
// The TUI prints these into the transcript; every other mode had nowhere to put
// them, so the watcher built the event and the notifier discarded it. That is
// the worst of the three options — the config is being watched, the reload did
// fail, and the run says nothing — because the next symptom is a tool call
// failing for a reason that looks unrelated.
//
// Only failures are reported, matching the TUI: a reload that works is meant to
// be invisible, or every save of an unrelated key announces itself.
func mcpReloadWriter(w io.Writer) func(mcp.ReloadEvent) {
	return func(ev mcp.ReloadEvent) {
		switch {
		case !ev.Failed():
			return
		case ev.ConfigErr != "":
			// Say the old servers survived. Without it the natural reading is
			// that MCP is now down, and the useful fact is the opposite.
			fmt.Fprintln(w, "warning: mcp config not reloaded:", ev.ConfigErr,
				"— the previously loaded servers are still running")
		default:
			fmt.Fprintf(w, "warning: mcp reload: %d server(s) failed to start\n", len(ev.ServerErrs))
			for _, e := range ev.ServerErrs {
				fmt.Fprintln(w, "  "+e)
			}
		}
	}
}
