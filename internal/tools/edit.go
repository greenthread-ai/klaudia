package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// EditInput is the Edit tool's input.
type EditInput struct {
	FilePath   string `json:"file_path" jsonschema:"description=The absolute path to the file to modify"`
	OldString  string `json:"old_string" jsonschema:"description=The text to replace"`
	NewString  string `json:"new_string" jsonschema:"description=The text to replace it with (must differ from old_string)"`
	ReplaceAll bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all occurrences (default false: old_string must be unique)"`
}

// Edit performs an exact string replacement in a file, mirroring the JS Edit
// tool: old_string must be unique unless replace_all is set.
type Edit struct {
	schema *schema.Schema
}

// NewEdit constructs the Edit tool.
func NewEdit() (*Edit, error) {
	s, err := schema.For[EditInput]()
	if err != nil {
		return nil, fmt.Errorf("edit: build schema: %w", err)
	}
	return &Edit{schema: s}, nil
}

func (e *Edit) Name() string { return "Edit" }

func (e *Edit) Description(context.Context) (string, error) {
	return "Performs an exact string replacement in a file. file_path must be absolute. " +
		"old_string must match exactly (including whitespace) and be unique in the file, " +
		"unless replace_all is true. new_string must differ from old_string.", nil
}

func (e *Edit) InputSchema() json.RawMessage { return e.schema.Raw }

func (e *Edit) ValidateInput(raw json.RawMessage) error {
	if err := e.schema.Validate(raw); err != nil {
		return err
	}
	var in EditInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if !filepath.IsAbs(in.FilePath) {
		return fmt.Errorf("file_path must be absolute, got %q", in.FilePath)
	}
	if in.OldString == in.NewString {
		return fmt.Errorf("old_string and new_string must differ")
	}
	return nil
}

// PermissionRequest: the rule specifier is the target file path.
func (e *Edit) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in EditInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.FilePath}
}

// CheckPermissions: Edit is a file-mutating (edit-class) tool.
func (e *Edit) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return editClassDecision(pctx)
}

func normalizeEditStrings(oldString, newString, content string) (string, string) {
	oldNorm := normalizeEditNewlines(oldString, content)
	newNorm := newString
	if oldNorm != oldString {
		switch {
		case strings.Contains(oldNorm, "\r\n") && !strings.Contains(oldString, "\r\n"):
			newNorm = strings.ReplaceAll(strings.ReplaceAll(newString, "\r\n", "\n"), "\n", "\r\n")
		case !strings.Contains(oldNorm, "\r\n") && strings.Contains(oldString, "\r\n"):
			newNorm = strings.ReplaceAll(newString, "\r\n", "\n")
		}
	}
	return oldNorm, newNorm
}

func normalizeEditNewlines(s, content string) string {
	if strings.Contains(content, s) {
		return s
	}
	if strings.Contains(s, "\r\n") && !strings.Contains(content, "\r\n") {
		lf := strings.ReplaceAll(s, "\r\n", "\n")
		if strings.Contains(content, lf) {
			return lf
		}
	}
	if strings.Contains(s, "\n") && strings.Contains(content, "\r\n") {
		crlf := strings.ReplaceAll(s, "\n", "\r\n")
		if strings.Contains(content, crlf) {
			return crlf
		}
	}
	return s
}

func editNotFoundMessage(content, oldString string) string {
	msg := "old_string not found in file. No edits were made."
	if suggestion := editHint(content, oldString); suggestion != "" {
		msg += " " + suggestion
	}
	return msg
}

func editHint(content, oldString string) string {
	trimmed := strings.TrimSpace(oldString)
	if trimmed == "" {
		return "old_string is empty or whitespace-only."
	}
	if strings.Contains(content, trimmed) {
		return "Hint: the trimmed text exists; check leading/trailing whitespace or indentation."
	}
	return "Hint: old_string must match exactly, including whitespace. Use Read immediately before Edit and include surrounding unchanged context."
}

func (e *Edit) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in EditInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(in.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Result{{Content: fmt.Sprintf("File does not exist: %s", in.FilePath), IsError: true}}, nil
		}
		return []Result{{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}}, nil
	}
	content := string(data)

	oldString, newString := normalizeEditStrings(in.OldString, in.NewString, content)
	count := strings.Count(content, oldString)
	if count == 0 {
		return []Result{{Content: editNotFoundMessage(content, oldString), IsError: true}}, nil
	}
	if count > 1 && !in.ReplaceAll {
		return []Result{{Content: fmt.Sprintf("old_string is not unique (%d matches). Provide more context or set replace_all=true.", count), IsError: true}}, nil
	}

	var updated string
	if in.ReplaceAll {
		updated = strings.ReplaceAll(content, oldString, newString)
	} else {
		updated = strings.Replace(content, oldString, newString, 1)
	}

	// Preserve the original file mode.
	info, statErr := os.Stat(in.FilePath)
	mode := os.FileMode(0o644)
	if statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(in.FilePath, []byte(updated), mode); err != nil {
		return []Result{{Content: fmt.Sprintf("Error writing file: %v", err), IsError: true}}, nil
	}

	n := 1
	if in.ReplaceAll {
		n = count
	}
	return []Result{{Content: fmt.Sprintf("Edited %s (%d replacement(s))", in.FilePath, n)}}, nil
}
