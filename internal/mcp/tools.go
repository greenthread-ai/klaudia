package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// mcpPermission is the intrinsic decision for MCP tools.
//
// When the trust model is enforcing, MCP calls proceed without a prompt. The
// host gate runs in front of every tool call and is what actually protects the
// machine; asking again here made MCP the one door where the zone model was
// ignored and the user was handed a per-tool consent decision instead. That
// decision was not answerable in any useful way — "may godot_game_time run?"
// carries no information the user has — and it did not even accumulate, because
// approvals key on the qualified name: renaming a server, or reaching for the
// twenty-second tool on it, started again from nothing.
//
// What this gives up is real and worth stating. The gate classifies tool calls
// by reading their inputs, and it has no model of what an MCP server does, so
// an MCP call raises no concerns and is allowed. Trusting the zone model here
// means trusting the servers in .mcp.json roughly as much as the shell —
// which is the same bet as running them at all.
//
// Plan mode allows what only reads, as it does for shell commands, and refuses
// the rest. readOnly is the readOnlyHint annotation or the .mcp.json override
// (see ReadOnly). It used to refuse every MCP call, which left the read-only
// sub-agents — which run in plan mode — holding read-only MCP tools they could
// never call: the feature the filter in internal/subagent exists for did not
// work. Trusting the declaration grants nothing new: outside plan mode the
// same servers' tools run without asking.
func mcpPermission(pctx permission.Context, readOnly bool) permission.Decision {
	if permission.CurrentMode(pctx) == permission.ModePlan && !readOnly {
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; this MCP tool is not marked read-only"}
	}
	return permission.Decision{Behavior: permission.Allow}
}

// mcpTool adapts a single MCP server tool to the Klaudia Tool interface. Its
// name is namespaced "mcp__<server>__<tool>" to avoid collisions.
type mcpTool struct {
	qualifiedName string
	remoteName    string
	description   string
	inputSchema   json.RawMessage
	server        *Server
	readOnly      bool
	// mgr is held so a call can relaunch its own server; see Execute.
	mgr *Manager
}

func (t *mcpTool) Name() string                                { return t.qualifiedName }
func (t *mcpTool) Description(context.Context) (string, error) { return t.description, nil }
func (t *mcpTool) InputSchema() json.RawMessage                { return t.inputSchema }

// ReadOnly reports that this tool only reads, as declared by the server's
// readOnlyHint annotation or as overridden per server in .mcp.json.
//
// It exists so the read-only sub-agents can be given MCP tools without being
// given the ability to write. A tool that says nothing is not read-only: the
// annotation is optional in the protocol, and the safe reading of silence is
// that the author never thought about it.
//
// This governs which tools a read-only sub-agent is handed, and which MCP
// tools plan mode allows (mcpPermission). It is not a claim that calling the
// tool is safe — nothing here verifies what a server does, and outside plan
// mode the main agent calls MCP tools without asking, read-only or not.
func (t *mcpTool) ReadOnly() bool { return t.readOnly }

// ValidateInput is a no-op beyond JSON well-formedness; the server validates
// against its own schema.
func (t *mcpTool) ValidateInput(raw json.RawMessage) error {
	if !json.Valid(raw) {
		return fmt.Errorf("input is not valid JSON")
	}
	return nil
}

func (t *mcpTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{Specifier: t.qualifiedName}
}

func (t *mcpTool) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return mcpPermission(pctx, t.readOnly)
}

func (t *mcpTool) Execute(ctx context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	var args any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	sess := t.server.sess()
	if sess == nil {
		// A server that failed to launch at startup, or was disconnected.
		// Worth one attempt before telling the model it is unusable: the
		// common cause is transient, and the alternative is a dead tool for
		// the rest of the session unless the user notices and runs /mcp.
		if !t.revive(ctx) {
			return []tools.Result{{Content: fmt.Sprintf("MCP server %q is disconnected and could not be restarted; reconnect it with /mcp.", t.server.Name), IsError: true}}, nil
		}
		sess = t.server.sess()
	}
	res, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: t.remoteName, Arguments: args})
	if err == nil {
		return []tools.Result{resultOf(res)}, nil
	}

	// The call failed on the wire. revive probes liveness first, so a bad
	// argument or an unknown tool — which also arrive as errors — cannot
	// restart a healthy server.
	if !t.revive(ctx) {
		return []tools.Result{{Content: fmt.Sprintf("MCP call failed: %v", err), IsError: true}}, nil
	}

	// The server is back. Whether to run the call again is not a performance
	// question, it is a correctness one: the failure may have happened on the
	// way back from a call that already ran, and nothing on the wire
	// distinguishes that from one that never arrived. A read can be repeated
	// at worst wastefully; anything else is reported, with the ambiguity
	// stated, so the model checks rather than silently doing it twice.
	if !t.readOnly {
		return []tools.Result{{Content: fmt.Sprintf(
			"MCP call failed: %v. Server %q had stopped responding and has been restarted. "+
				"The call may or may not have taken effect before it died — check the state before retrying.",
			err, t.server.Name), IsError: true}}, nil
	}
	sess = t.server.sess()
	if sess == nil {
		return []tools.Result{{Content: fmt.Sprintf("MCP call failed: %v", err), IsError: true}}, nil
	}
	res, err = sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: t.remoteName, Arguments: args})
	if err != nil {
		return []tools.Result{{Content: fmt.Sprintf("MCP call failed after restarting server %q: %v", t.server.Name, err), IsError: true}}, nil
	}
	return []tools.Result{resultOf(res)}, nil
}

// revive relaunches the tool's server if it has died. It is a no-op returning
// false when the tool was built without a Manager (tests that wire a Server
// directly), so the old behaviour stands there.
func (t *mcpTool) revive(ctx context.Context) bool {
	if t.mgr == nil {
		return false
	}
	return t.mgr.revive(ctx, t.server)
}

// Tools lists every connected server's tools and wraps them. Servers that fail
// to list are skipped.
func (m *Manager) Tools(ctx context.Context) []tools.Tool {
	var out []tools.Tool
	for _, srv := range m.Servers() {
		sess := srv.sess()
		if sess == nil {
			continue
		}
		// Every page: a server that paginates its list used to lose everything
		// after the first one. A listing that fails part-way contributes
		// nothing, as a failed listing always has.
		var listed []*mcpsdk.Tool
		failed := false
		for rt, err := range sess.Tools(ctx, nil) {
			if err != nil {
				failed = true
				break
			}
			listed = append(listed, rt)
		}
		if failed {
			continue
		}
		// An override, when present, decides for every tool on the server and
		// the annotations are not consulted at all — in either direction.
		cfg, _ := m.serverConfig(srv.Name)
		override := cfg.ReadOnly
		for _, rt := range listed {
			schema, _ := json.Marshal(rt.InputSchema)
			if len(schema) == 0 || string(schema) == "null" {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			readOnly := rt.Annotations != nil && rt.Annotations.ReadOnlyHint
			if override != nil {
				readOnly = *override
			}
			out = append(out, &mcpTool{
				qualifiedName: qualifiedName(srv.Name, rt.Name),
				remoteName:    rt.Name,
				description:   rt.Description,
				inputSchema:   schema,
				server:        srv,
				readOnly:      readOnly,
				mgr:           m,
			})
		}
	}
	return out
}

// ResourceTools returns the ListMcpResources and ReadMcpResource tools backed
// by this manager.
func (m *Manager) ResourceTools() ([]tools.Tool, error) {
	list, err := newListResourcesTool(m)
	if err != nil {
		return nil, err
	}
	read, err := newReadResourceTool(m)
	if err != nil {
		return nil, err
	}
	return []tools.Tool{list, read}, nil
}

// findServer returns the connected server with the given name.
func (m *Manager) findServer(name string) *Server { return m.find(name) }

// formatResourceList renders the resources of all servers as text.
func (m *Manager) formatResourceList(ctx context.Context) string {
	var b strings.Builder
	for _, srv := range m.Servers() {
		sess := srv.sess()
		if sess == nil {
			continue
		}
		for r, err := range sess.Resources(ctx, nil) { // every page
			if err != nil {
				break
			}
			fmt.Fprintf(&b, "%s\t%s\t%s\n", srv.Name, r.URI, r.Name)
		}
	}
	if b.Len() == 0 {
		return "No MCP resources available."
	}
	return strings.TrimRight(b.String(), "\n")
}

// maxToolName is the longest tool name the Anthropic API accepts.
const maxToolName = 64

// qualifiedName namespaces a server's tool as mcp__<server>__<tool>, in the
// form the Anthropic API accepts: ^[a-zA-Z0-9_-]{1,64}$. MCP allows tool names
// with dots and up to 128 characters, and server names are free text from
// .mcp.json — "my.server" serving "shot.png" became mcp__my.server__shot.png,
// and every request after ToolSearch revealed it was refused. Anything outside
// the set becomes "_"; a name still too long keeps its start and gains a hash
// of the whole, so two long names cannot collide. Valid names are unchanged.
// The call itself uses the tool's real name (remoteName).
func qualifiedName(server, tool string) string {
	clean := func(s string) string {
		return strings.Map(func(r rune) rune {
			if r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
				return r
			}
			return '_'
		}, s)
	}
	name := "mcp__" + clean(server) + "__" + clean(tool)
	if len(name) <= maxToolName {
		return name
	}
	sum := sha256.Sum256([]byte(server + "\x00" + tool))
	suffix := "_" + hex.EncodeToString(sum[:4])
	return name[:maxToolName-len(suffix)] + suffix
}

// ServerName is the server this tool belongs to — what /mcp counts by, rather
// than parsing a name that may have been rewritten to fit the API.
func (t *mcpTool) ServerName() string { return t.server.Name }
