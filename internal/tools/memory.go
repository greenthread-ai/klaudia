package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread/klaudia/internal/memory"
	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// MemoryInput is the Memory tool's input.
type MemoryInput struct {
	Operation string `json:"operation" jsonschema:"description=One of: search, add, view"`
	Query     string `json:"query,omitempty" jsonschema:"description=Search terms (for operation=search)"`
	Content   string `json:"content,omitempty" jsonschema:"description=The note to store (for operation=add)"`
}

// Memory lets the agent recall, search, and store long-term notes that persist
// across sessions — so it can remember project context, decisions, and gotchas
// instead of re-deriving or re-asking.
type Memory struct {
	schema *schema.Schema
	store  *memory.Store
}

// NewMemory constructs the Memory tool backed by store.
func NewMemory(store *memory.Store) (*Memory, error) {
	s, err := schema.For[MemoryInput]()
	if err != nil {
		return nil, fmt.Errorf("memory: build schema: %w", err)
	}
	return &Memory{schema: s, store: store}, nil
}

func (m *Memory) Name() string { return "Memory" }

func (m *Memory) Description(context.Context) (string, error) {
	return "Your persistent memory across sessions. Use it to recall prior context before " +
		"asking the user or re-investigating, and to store durable facts (decisions, conventions, " +
		"gotchas) for later. operation: \"search\" (find notes matching query terms), " +
		"\"view\" (read all notes), \"add\" (store a new note via content). " +
		"Search early when resuming work or when a task references past decisions.", nil
}

func (m *Memory) InputSchema() json.RawMessage { return m.schema.Raw }

func (m *Memory) ValidateInput(raw json.RawMessage) error {
	if err := m.schema.Validate(raw); err != nil {
		return err
	}
	var in MemoryInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	switch in.Operation {
	case "search", "view":
	case "add":
		if strings.TrimSpace(in.Content) == "" {
			return fmt.Errorf("operation \"add\" requires content")
		}
	default:
		return fmt.Errorf("operation %q is invalid (want search|view|add)", in.Operation)
	}
	return nil
}

// PermissionRequest: memory is the agent's own scratch space, not user files.
func (m *Memory) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (m *Memory) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (m *Memory) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in MemoryInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	switch in.Operation {
	case "add":
		if err := m.store.Add(in.Content); err != nil {
			return []Result{{Content: "Failed to store memory: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: "Stored to memory."}}, nil
	case "search":
		matches, err := m.store.Search(in.Query)
		if err != nil {
			return []Result{{Content: "Memory search failed: " + err.Error(), IsError: true}}, nil
		}
		if len(matches) == 0 {
			return []Result{{Content: "No memories matched."}}, nil
		}
		return []Result{{Content: "Recalled memories:\n- " + strings.Join(matches, "\n- ")}}, nil
	default: // view
		idx, err := m.store.Index()
		if err != nil {
			return []Result{{Content: "Failed to read memory: " + err.Error(), IsError: true}}, nil
		}
		if strings.TrimSpace(idx) == "" {
			return []Result{{Content: "Memory is empty."}}, nil
		}
		return []Result{{Content: idx}}, nil
	}
}
