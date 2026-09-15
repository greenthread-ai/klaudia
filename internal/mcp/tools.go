package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
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
// which is the same bet as running them at all, and is why this follows the
// trust posture rather than being unconditional.
//
// Without trust enforcing, the old behaviour stands: ask in interactive modes,
// refuse where there is nobody to ask.
func mcpPermission(pctx permission.Context) permission.Decision {
	if permission.CurrentMode(pctx) == permission.ModePlan {
		// Plan mode is read-only for every tool, trusted or not.
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; MCP tools are not allowed"}
	}
	if permission.IsTrusting(pctx) {
		return permission.Decision{Behavior: permission.Allow}
	}
	if permission.CurrentMode(pctx) == permission.ModeDontAsk {
		return permission.Decision{Behavior: permission.Deny, Message: "not pre-approved (dontAsk mode)"}
	}
	return permission.Decision{Behavior: permission.Ask}
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
// This governs which tools a read-only sub-agent is handed. It is not a claim
// that calling the tool is safe — nothing here verifies what a server does, and
// under an enforcing trust posture the main agent calls MCP tools without
// asking, read-only or not.
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
	return mcpPermission(pctx)
}

func (t *mcpTool) Execute(ctx context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	var args any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	sess := t.server.sess()
	if sess == nil {
		return []tools.Result{{Content: fmt.Sprintf("MCP server %q is disconnected; reconnect it with /mcp.", t.server.Name), IsError: true}}, nil
	}
	res, err := sess.CallTool(ctx, &mcpsdk.CallToolParams{Name: t.remoteName, Arguments: args})
	if err != nil {
		return []tools.Result{{Content: fmt.Sprintf("MCP call failed: %v", err), IsError: true}}, nil
	}
	return []tools.Result{{Content: textOf(res.Content), IsError: res.IsError}}, nil
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
		res, err := sess.ListTools(ctx, &mcpsdk.ListToolsParams{})
		if err != nil {
			continue
		}
		// An override, when present, decides for every tool on the server and
		// the annotations are not consulted at all — in either direction.
		cfg, _ := m.serverConfig(srv.Name)
		override := cfg.ReadOnly
		for _, rt := range res.Tools {
			schema, _ := json.Marshal(rt.InputSchema)
			if len(schema) == 0 || string(schema) == "null" {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			readOnly := rt.Annotations != nil && rt.Annotations.ReadOnlyHint
			if override != nil {
				readOnly = *override
			}
			out = append(out, &mcpTool{
				qualifiedName: fmt.Sprintf("mcp__%s__%s", srv.Name, rt.Name),
				remoteName:    rt.Name,
				description:   rt.Description,
				inputSchema:   schema,
				server:        srv,
				readOnly:      readOnly,
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
		res, err := sess.ListResources(ctx, &mcpsdk.ListResourcesParams{})
		if err != nil {
			continue
		}
		for _, r := range res.Resources {
			fmt.Fprintf(&b, "%s\t%s\t%s\n", srv.Name, r.URI, r.Name)
		}
	}
	if b.Len() == 0 {
		return "No MCP resources available."
	}
	return strings.TrimRight(b.String(), "\n")
}
