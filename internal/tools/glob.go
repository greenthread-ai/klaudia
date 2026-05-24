package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread/klaudia/internal/native/search"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// GlobInput is the Glob tool's input.
type GlobInput struct {
	Pattern string `json:"pattern" jsonschema:"description=The glob pattern to match files against (e.g. **/*.go)"`
	Path    string `json:"path,omitempty" jsonschema:"description=The directory to search in (defaults to the current working directory)"`
}

// Glob finds files matching a glob pattern, sorted by modification time.
type Glob struct {
	schema *schema.Schema
}

// NewGlob constructs the Glob tool.
func NewGlob() (*Glob, error) {
	s, err := schema.For[GlobInput]()
	if err != nil {
		return nil, fmt.Errorf("glob: build schema: %w", err)
	}
	return &Glob{schema: s}, nil
}

func (g *Glob) Name() string { return "Glob" }

func (g *Glob) Description(context.Context) (string, error) {
	return "Fast file pattern matching. Supports glob patterns like \"**/*.js\" or \"src/**/*.ts\". " +
		"Returns matching file paths sorted by modification time (newest first).", nil
}

func (g *Glob) InputSchema() json.RawMessage { return g.schema.Raw }

func (g *Glob) ValidateInput(raw json.RawMessage) error { return g.schema.Validate(raw) }

// PermissionRequest: Glob is read-only.
func (g *Glob) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (g *Glob) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (g *Glob) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in GlobInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	root := in.Path
	if root == "" {
		root = tctx.WorkingDir
	}
	files, err := search.Glob(search.GlobOptions{Root: root, Pattern: in.Pattern})
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error: %v", err), IsError: true}}, nil
	}
	if len(files) == 0 {
		return []Result{{Content: "No files found"}}, nil
	}
	return []Result{{Content: strings.Join(files, "\n")}}, nil
}
