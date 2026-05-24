package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/tools"
)

// mcpPermission is the intrinsic decision for MCP tools: ask in interactive
// modes, blocked in plan/dontAsk. (bypass is handled upstream.) MCP tools are
// external code, so they are never auto-allowed by mode alone.
func mcpPermission(pctx permission.Context) permission.Decision {
	switch pctx.Mode {
	case permission.ModePlan:
		return permission.Decision{Behavior: permission.Deny, Message: "plan mode is read-only; MCP tools are not allowed"}
	case permission.ModeDontAsk:
		return permission.Decision{Behavior: permission.Deny, Message: "not pre-approved (dontAsk mode)"}
	default:
		return permission.Decision{Behavior: permission.Ask}
	}
}

// mcpTool adapts a single MCP server tool to the Klaudia Tool interface. Its
// name is namespaced "mcp__<server>__<tool>" to avoid collisions.
type mcpTool struct {
	qualifiedName string
	remoteName    string
	description   string
	inputSchema   json.RawMessage
	server        *Server
}

func (t *mcpTool) Name() string                              { return t.qualifiedName }
func (t *mcpTool) Description(context.Context) (string, error) { return t.description, nil }
func (t *mcpTool) InputSchema() json.RawMessage              { return t.inputSchema }

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
	if !t.server.Connected() {
		return []tools.Result{{Content: fmt.Sprintf("MCP server %q is disconnected; reconnect it with /mcp.", t.server.Name), IsError: true}}, nil
	}
	res, err := t.server.session.CallTool(ctx, &mcpsdk.CallToolParams{Name: t.remoteName, Arguments: args})
	if err != nil {
		return []tools.Result{{Content: fmt.Sprintf("MCP call failed: %v", err), IsError: true}}, nil
	}
	return []tools.Result{{Content: textOf(res.Content), IsError: res.IsError}}, nil
}

// Tools lists every connected server's tools and wraps them. Servers that fail
// to list are skipped.
func (m *Manager) Tools(ctx context.Context) []tools.Tool {
	var out []tools.Tool
	for _, srv := range m.servers {
		if !srv.Connected() {
			continue
		}
		res, err := srv.session.ListTools(ctx, &mcpsdk.ListToolsParams{})
		if err != nil {
			continue
		}
		for _, rt := range res.Tools {
			schema, _ := json.Marshal(rt.InputSchema)
			if len(schema) == 0 || string(schema) == "null" {
				schema = json.RawMessage(`{"type":"object"}`)
			}
			out = append(out, &mcpTool{
				qualifiedName: fmt.Sprintf("mcp__%s__%s", srv.Name, rt.Name),
				remoteName:    rt.Name,
				description:   rt.Description,
				inputSchema:   schema,
				server:        srv,
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
func (m *Manager) findServer(name string) *Server {
	for _, s := range m.servers {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// formatResourceList renders the resources of all servers as text.
func (m *Manager) formatResourceList(ctx context.Context) string {
	var b strings.Builder
	for _, srv := range m.servers {
		if !srv.Connected() {
			continue
		}
		res, err := srv.session.ListResources(ctx, &mcpsdk.ListResourcesParams{})
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
