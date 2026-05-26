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
	Operation string `json:"operation" jsonschema:"enum=search,enum=add,enum=view,description=Operation to perform: search recalls matching notes; view reads all session notes; add stores content"`
	Scope     string `json:"scope,omitempty" jsonschema:"enum=session,enum=project,description=Where to write for operation=add: session (default) writes .klaudia/MEMORY.md; project writes .klaudia/KNOWLEDGE.md"`
	Query     string `json:"query,omitempty" jsonschema:"description=Search terms (required for operation=search)"`
	Content   string `json:"content,omitempty" jsonschema:"description=The note to store (required for operation=add)"`
}

// Memory lets the agent recall, search, and store long-term notes that persist
// across sessions — so it can remember project context, decisions, and gotchas
// instead of re-deriving or re-asking.
type Memory struct {
	schema *schema.Schema
	store  *memory.Store
	cwd    string
}

// NewMemory constructs the Memory tool backed by store.
func NewMemory(store *memory.Store) (*Memory, error) {
	return NewMemoryForProject(store, "")
}

// NewMemoryForProject constructs the Memory tool with a project cwd for
// project-scope writes to .klaudia/KNOWLEDGE.md.
func NewMemoryForProject(store *memory.Store, cwd string) (*Memory, error) {
	s, err := schema.For[MemoryInput]()
	if err != nil {
		return nil, fmt.Errorf("memory: build schema: %w", err)
	}
	return &Memory{schema: s, store: store, cwd: cwd}, nil
}

func (m *Memory) Name() string { return "Memory" }

func (m *Memory) Description(context.Context) (string, error) {
	return "Your persistent memory across sessions. Use it to recall prior context before " +
		"asking the user or re-investigating, and to store durable facts (decisions, conventions, " +
		"gotchas) for later. operation: \"search\" (find notes matching query terms — searches the " +
		"MEMORY.md index and the .klaudia/memory/*.md detail notes; a detail hit is tagged " +
		"\"file.md: …\", so Read that file for full context), \"view\" (read all session notes), " +
		"\"add\" (store a new note via content). " +
		"For operation=add, scope may be \"session\" (default, .klaudia/MEMORY.md) " +
		"or \"project\" (.klaudia/KNOWLEDGE.md for durable project understanding). " +
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
	if in.Scope != "" && in.Scope != "session" && in.Scope != "project" {
		return fmt.Errorf("scope %q is invalid (want session|project)", in.Scope)
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
		if in.Scope == "project" {
			if strings.TrimSpace(m.cwd) == "" {
				return []Result{{Content: "Failed to store project knowledge: project cwd is unavailable", IsError: true}}, nil
			}
			if err := memory.AddKnowledge(m.cwd, in.Content); err != nil {
				return []Result{{Content: "Failed to store project knowledge: " + err.Error(), IsError: true}}, nil
			}
			return []Result{{Content: "Stored to project knowledge (.klaudia/KNOWLEDGE.md)."}}, nil
		}
		if err := m.store.Add(in.Content); err != nil {
			return []Result{{Content: "Failed to store memory: " + err.Error(), IsError: true}}, nil
		}
		return []Result{{Content: "Stored to session memory."}}, nil
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
