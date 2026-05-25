package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// NotebookEditInput is the NotebookEdit tool's input.
type NotebookEditInput struct {
	NotebookPath string `json:"notebook_path" jsonschema:"description=Path to the .ipynb file (absolute, or relative to the working directory)"`
	NewSource    string `json:"new_source" jsonschema:"description=The new cell source (ignored for delete)"`
	CellID       string `json:"cell_id,omitempty" jsonschema:"description=Target cell id; or a 0-based index. Empty inserts at the top."`
	CellType     string `json:"cell_type,omitempty" jsonschema:"description=code or markdown (required for insert)"`
	EditMode     string `json:"edit_mode,omitempty" jsonschema:"description=replace (default), insert, or delete"`
}

// NotebookEdit edits cells of a Jupyter notebook (.ipynb), mirroring the JS
// NotebookEdit tool: replace a cell's source, insert a new cell, or delete one.
type NotebookEdit struct {
	schema *schema.Schema
}

// NewNotebookEdit constructs the NotebookEdit tool.
func NewNotebookEdit() (*NotebookEdit, error) {
	s, err := schema.For[NotebookEditInput]()
	if err != nil {
		return nil, fmt.Errorf("notebookedit: build schema: %w", err)
	}
	return &NotebookEdit{schema: s}, nil
}

func (n *NotebookEdit) Name() string { return "NotebookEdit" }

func (n *NotebookEdit) Description(context.Context) (string, error) {
	return "Edit a Jupyter notebook (.ipynb). edit_mode: \"replace\" sets a cell's source, " +
		"\"insert\" adds a new cell (cell_type required), \"delete\" removes a cell. " +
		"cell_id matches a cell's id or is a 0-based index.", nil
}

func (n *NotebookEdit) InputSchema() json.RawMessage { return n.schema.Raw }

func (n *NotebookEdit) ValidateInput(raw json.RawMessage) error {
	if err := n.schema.Validate(raw); err != nil {
		return err
	}
	var in NotebookEditInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.NotebookPath) == "" {
		return fmt.Errorf("notebook_path is required")
	}
	switch in.editMode() {
	case "replace", "delete":
	case "insert":
		if in.CellType != "code" && in.CellType != "markdown" {
			return fmt.Errorf("insert requires cell_type to be \"code\" or \"markdown\"")
		}
	default:
		return fmt.Errorf("edit_mode %q is invalid (want replace|insert|delete)", in.EditMode)
	}
	return nil
}

// PermissionRequest: the rule specifier is the notebook path.
func (n *NotebookEdit) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in NotebookEditInput
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.NotebookPath}
}

// CheckPermissions: NotebookEdit mutates a file (edit-class).
func (n *NotebookEdit) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return editClassDecision(pctx)
}

func (in NotebookEditInput) editMode() string {
	if in.EditMode == "" {
		return "replace"
	}
	return in.EditMode
}

func (n *NotebookEdit) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in NotebookEditInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	in.NotebookPath = resolvePath(tctx, in.NotebookPath) // accept paths relative to the working dir

	data, err := os.ReadFile(in.NotebookPath)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading notebook: %v", err), IsError: true}}, nil
	}
	var nb map[string]any
	if err := json.Unmarshal(data, &nb); err != nil {
		return []Result{{Content: fmt.Sprintf("Notebook is not valid JSON: %v", err), IsError: true}}, nil
	}
	cells, _ := nb["cells"].([]any)

	idx, found := findCell(cells, in.CellID)

	switch in.editMode() {
	case "replace":
		if !found {
			return []Result{{Content: fmt.Sprintf("Cell %q not found", in.CellID), IsError: true}}, nil
		}
		cell, _ := cells[idx].(map[string]any)
		cell["source"] = sourceLines(in.NewSource)
		if in.CellType != "" {
			cell["cell_type"] = in.CellType
		}
	case "insert":
		newCell := map[string]any{
			"cell_type": in.CellType,
			"metadata":  map[string]any{},
			"source":    sourceLines(in.NewSource),
		}
		if in.CellType == "code" {
			newCell["outputs"] = []any{}
			newCell["execution_count"] = nil
		}
		at := 0
		if found {
			at = idx + 1
		}
		cells = insertAt(cells, at, newCell)
	case "delete":
		if !found {
			return []Result{{Content: fmt.Sprintf("Cell %q not found", in.CellID), IsError: true}}, nil
		}
		cells = append(cells[:idx], cells[idx+1:]...)
	}
	nb["cells"] = cells

	out, err := json.MarshalIndent(nb, "", " ")
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error encoding notebook: %v", err), IsError: true}}, nil
	}
	if err := os.WriteFile(in.NotebookPath, append(out, '\n'), 0o644); err != nil {
		return []Result{{Content: fmt.Sprintf("Error writing notebook: %v", err), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf("Notebook %s updated (%s)", in.NotebookPath, in.editMode())}}, nil
}

// findCell locates a cell by its "id" field or, failing that, by 0-based index.
func findCell(cells []any, id string) (int, bool) {
	if id == "" {
		return 0, false
	}
	for i, c := range cells {
		if m, ok := c.(map[string]any); ok {
			if cid, _ := m["id"].(string); cid == id {
				return i, true
			}
		}
	}
	// Try numeric index.
	var idx int
	if _, err := fmt.Sscanf(id, "%d", &idx); err == nil && idx >= 0 && idx < len(cells) {
		return idx, true
	}
	return 0, false
}

// insertAt inserts cell into s at position i (clamped to [0,len]).
func insertAt(s []any, i int, cell any) []any {
	if i < 0 {
		i = 0
	}
	if i > len(s) {
		i = len(s)
	}
	s = append(s, nil)
	copy(s[i+1:], s[i:])
	s[i] = cell
	return s
}

// sourceLines splits source into the ipynb line list (each line keeps its
// trailing newline except the last), matching the .ipynb "source" convention.
func sourceLines(src string) []any {
	if src == "" {
		return []any{}
	}
	parts := strings.SplitAfter(src, "\n")
	// SplitAfter leaves a trailing "" when src ends with \n; drop it.
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}
