package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
	"github.com/greenthread/klaudia/internal/tools"
)

// --- ListMcpResources ---

type listResourcesInput struct{}

type listResourcesTool struct {
	schema *schema.Schema
	mgr    *Manager
}

func newListResourcesTool(m *Manager) (*listResourcesTool, error) {
	s, err := schema.For[listResourcesInput]()
	if err != nil {
		return nil, err
	}
	return &listResourcesTool{schema: s, mgr: m}, nil
}

func (t *listResourcesTool) Name() string { return "ListMcpResources" }
func (t *listResourcesTool) Description(context.Context) (string, error) {
	return "List the resources available from connected MCP servers, as lines of \"server\\turi\\tname\".", nil
}
func (t *listResourcesTool) InputSchema() json.RawMessage    { return t.schema.Raw }
func (t *listResourcesTool) ValidateInput(json.RawMessage) error { return nil }
func (t *listResourcesTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (t *listResourcesTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (t *listResourcesTool) Execute(ctx context.Context, _ tools.Context, _ json.RawMessage) ([]tools.Result, error) {
	return []tools.Result{{Content: t.mgr.formatResourceList(ctx)}}, nil
}

// --- ReadMcpResource ---

type readResourceInput struct {
	Server string `json:"server" jsonschema:"description=The MCP server name"`
	URI    string `json:"uri" jsonschema:"description=The resource URI to read"`
}

type readResourceTool struct {
	schema *schema.Schema
	mgr    *Manager
}

func newReadResourceTool(m *Manager) (*readResourceTool, error) {
	s, err := schema.For[readResourceInput]()
	if err != nil {
		return nil, err
	}
	return &readResourceTool{schema: s, mgr: m}, nil
}

func (t *readResourceTool) Name() string { return "ReadMcpResource" }
func (t *readResourceTool) Description(context.Context) (string, error) {
	return "Read a resource from a connected MCP server by server name and URI.", nil
}
func (t *readResourceTool) InputSchema() json.RawMessage      { return t.schema.Raw }
func (t *readResourceTool) ValidateInput(raw json.RawMessage) error { return t.schema.Validate(raw) }
func (t *readResourceTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (t *readResourceTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (t *readResourceTool) Execute(ctx context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	var in readResourceInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	srv := t.mgr.findServer(in.Server)
	if srv == nil {
		return []tools.Result{{Content: fmt.Sprintf("Unknown MCP server %q", in.Server), IsError: true}}, nil
	}
	res, err := srv.session.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: in.URI})
	if err != nil {
		return []tools.Result{{Content: fmt.Sprintf("Read failed: %v", err), IsError: true}}, nil
	}
	var out string
	for _, c := range res.Contents {
		if c.Text != "" {
			if out != "" {
				out += "\n"
			}
			out += c.Text
		}
	}
	return []tools.Result{{Content: out}}, nil
}
