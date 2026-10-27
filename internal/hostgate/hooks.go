package hostgate

import (
	"context"
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/trust"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// ConfirmHooks asks the user, through approver, whether the hooks declared in
// file — given as their command lines — may run. approver must be non-nil:
// with no one to ask, the hook runner is handed no confirm function at all,
// which is how it tells "nobody was asked" from "the user said no".
//
// It reuses the approval path rather than inventing a second one: there is
// exactly one place a question can be put to the user, every frontend already
// implements it, and a headless run already has the right answer for "nobody
// is here" built in. The request is a host-change card because that is what it
// is — whether commands from a repository may run on this machine — and the
// card explains the scope rather than asking "allow Bash?".
//
// bypassPermissions is not special-cased. That mode turns off checks on the
// model's actions; it is not a statement that any repo Klaudia is pointed at
// may execute code. Someone who wants that can approve the hooks once.
func ConfirmHooks(ctx context.Context, approver agent.Approver, commands []string, file string) bool {
	summary := fmt.Sprintf("run %s declared by %s", plural(len(commands), "hook"), file)
	d := approver.Approve(ctx, agent.ApprovalRequest{
		ToolName:   "Hooks",
		Specifier:  file,
		Suggestion: summary,
		Detail: &HostChange{
			RepoConfig: RepoHooks,
			Summary:    summary,
			Reason: "Hooks are shell commands this repository wants run around your tool calls. " +
				"They run unconfined, as you.",
			// ZoneHost: this is a change to what runs on this machine, which
			// is the one zone that is never autonomous.
			Zone:     trust.ZoneHost,
			Paths:    []string{file},
			Commands: commands,
		},
	})
	return d.Behavior == permission.Allow
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// RepoConfig values: what a repository's config is asking to run.
const (
	RepoHooks = "hooks"
	RepoMCP   = "MCP servers"
)

// ConfirmMCP asks, as ConfirmHooks does, whether the MCP servers a
// repository's .mcp.json declares may start. lines describe each server.
func ConfirmMCP(ctx context.Context, approver agent.Approver, lines, files []string) bool {
	summary := fmt.Sprintf("start %s declared by %s", plural(len(lines), "MCP server"), strings.Join(files, ", "))
	d := approver.Approve(ctx, agent.ApprovalRequest{
		ToolName:   "MCP",
		Specifier:  strings.Join(files, ", "),
		Suggestion: summary,
		Detail: &HostChange{
			RepoConfig: RepoMCP,
			Summary:    summary,
			Reason: "MCP servers are programs this repository wants launched, or services it wants Klaudia to connect to, " +
				"whose tools the model can then call. A launched server runs unconfined, as you.",
			Zone:     trust.ZoneHost,
			Paths:    files,
			Commands: lines,
		},
	})
	return d.Behavior == permission.Allow
}
