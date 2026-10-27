package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/schema"
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
func (t *listResourcesTool) InputSchema() json.RawMessage        { return t.schema.Raw }
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
func (t *readResourceTool) InputSchema() json.RawMessage            { return t.schema.Raw }
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
	sess := srv.sess()
	if sess == nil {
		if !t.mgr.revive(ctx, srv) {
			return []tools.Result{{Content: fmt.Sprintf("MCP server %q is disconnected and could not be restarted; reconnect it with /mcp.", in.Server), IsError: true}}, nil
		}
		sess = srv.sess()
	}
	res, err := sess.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: in.URI})
	if err != nil {
		// Reading a resource has no side effects, so the retry needs none of
		// the care a tool call does (see mcpTool.Execute).
		if t.mgr.revive(ctx, srv) {
			if sess = srv.sess(); sess != nil {
				res, err = sess.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: in.URI})
			}
		}
	}
	if err != nil {
		return []tools.Result{{Content: fmt.Sprintf("Read failed: %v", err), IsError: true}}, nil
	}
	var r tools.Result
	var parts []string
	for _, c := range res.Contents {
		if c.Text == "" && visionTypes[c.MIMEType] {
			r.Images = append(r.Images, tools.ResultImage{MediaType: c.MIMEType, Base64: base64.StdEncoding.EncodeToString(c.Blob)})
		} else if t := resourceText(c); t != "" {
			parts = append(parts, t)
		}
	}
	r.Content = strings.Join(parts, "\n")
	return []tools.Result{r}, nil
}

// ResourceToolSchemas is tools.BuiltinSchemas for the two resource tools, which
// live here because they need the Manager. Built with no Manager: only the
// schema is read.
func ResourceToolSchemas() (map[string]json.RawMessage, error) {
	list, err := newListResourcesTool(nil)
	if err != nil {
		return nil, err
	}
	read, err := newReadResourceTool(nil)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{list.Name(): list.InputSchema(), read.Name(): read.InputSchema()}, nil
}
