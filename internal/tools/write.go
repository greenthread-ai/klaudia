package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// WriteInput is the Write tool's input.
type WriteInput struct {
	FilePath string `json:"file_path" jsonschema:"description=The path to the file to write (absolute, or relative to the working directory)"`
	Content  string `json:"content" jsonschema:"description=The content to write to the file"`
}

// Write writes (creating or overwriting) a file on the local filesystem,
// mirroring the JS Write tool.
type Write struct {
	schema *schema.Schema
}

// NewWrite constructs the Write tool.
func NewWrite() (*Write, error) {
	s, err := schema.For[WriteInput]()
	if err != nil {
		return nil, fmt.Errorf("write: build schema: %w", err)
	}
	return &Write{schema: s}, nil
}

func (w *Write) Name() string { return "Write" }

func (w *Write) Description(context.Context) (string, error) {
	return "Writes a file to the local filesystem, overwriting it if it already exists. " +
		"file_path may be absolute or relative to the working directory. Parent " +
		"directories are created as needed. Prefer Edit for modifying existing files.", nil
}

func (w *Write) InputSchema() json.RawMessage { return w.schema.Raw }

func (w *Write) ValidateInput(raw json.RawMessage) error {
	if err := w.schema.Validate(raw); err != nil {
		return err
	}
	var in WriteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.FilePath) == "" {
		return fmt.Errorf("file_path is required")
	}
	return nil
}

// PermissionRequest: the rule specifier is the target file path.
func (w *Write) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in WriteInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.FilePath}
}

// CheckPermissions: Write is a file-mutating (edit-class) tool.
func (w *Write) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return editClassDecision(pctx)
}

func (w *Write) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in WriteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	in.FilePath = resolvePath(tctx, in.FilePath) // accept paths relative to the working dir
	if dir := filepath.Dir(in.FilePath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return []Result{{Content: fmt.Sprintf("Error creating parent directory: %v", err), IsError: true}}, nil
		}
	}
	if err := os.WriteFile(in.FilePath, []byte(in.Content), 0o644); err != nil {
		return []Result{{Content: fmt.Sprintf("Error writing file: %v", err), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf("File written successfully to %s", in.FilePath)}}, nil
}
