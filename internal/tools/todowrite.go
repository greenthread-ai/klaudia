package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// TodoItem is one entry in the model's task list.
type TodoItem struct {
	Content    string `json:"content" jsonschema:"description=The task description (imperative form)"`
	Status     string `json:"status" jsonschema:"description=One of: pending, in_progress, completed"`
	ActiveForm string `json:"activeForm,omitempty" jsonschema:"description=Present-continuous form shown while in progress"`
}

// TodoInput is the TodoWrite tool's input: the full, updated todo list.
type TodoInput struct {
	Todos []TodoItem `json:"todos" jsonschema:"description=The complete, updated todo list"`
}

// TodoStore holds the current todo list for a session. It is safe for
// concurrent use so streaming/concurrent tool execution can update it.
type TodoStore struct {
	mu    sync.Mutex
	items []TodoItem
}

// Items returns a copy of the current todo list.
func (s *TodoStore) Items() []TodoItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TodoItem(nil), s.items...)
}

func (s *TodoStore) set(items []TodoItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = items
}

// TodoWrite replaces the session todo list, mirroring the JS TodoWrite tool.
type TodoWrite struct {
	schema *schema.Schema
	store  *TodoStore
}

// NewTodoWrite constructs the TodoWrite tool backed by store.
func NewTodoWrite(store *TodoStore) (*TodoWrite, error) {
	s, err := schema.For[TodoInput]()
	if err != nil {
		return nil, fmt.Errorf("todowrite: build schema: %w", err)
	}
	return &TodoWrite{schema: s, store: store}, nil
}

func (t *TodoWrite) Name() string { return "TodoWrite" }

func (t *TodoWrite) Description(context.Context) (string, error) {
	return "Update the task list. Pass the complete, updated list of todos each time. " +
		"Each todo has content (imperative), status (pending|in_progress|completed), and " +
		"activeForm (present-continuous). Keep exactly one task in_progress at a time.", nil
}

func (t *TodoWrite) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *TodoWrite) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in TodoInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	for i, it := range in.Todos {
		switch it.Status {
		case "pending", "in_progress", "completed":
		default:
			return fmt.Errorf("todos[%d].status %q is invalid (want pending|in_progress|completed)", i, it.Status)
		}
	}
	return nil
}

// PermissionRequest: TodoWrite has no filesystem/exec side effects.
func (t *TodoWrite) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *TodoWrite) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *TodoWrite) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in TodoInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	t.store.set(in.Todos)
	return []Result{{Content: renderTodos(in.Todos)}}, nil
}

// renderTodos formats the list with status checkboxes for the tool_result.
func renderTodos(todos []TodoItem) string {
	if len(todos) == 0 {
		return "Todo list cleared."
	}
	var b strings.Builder
	b.WriteString("Todos updated:\n")
	for _, it := range todos {
		box := "[ ]"
		switch it.Status {
		case "in_progress":
			box = "[~]"
		case "completed":
			box = "[x]"
		}
		fmt.Fprintf(&b, "%s %s\n", box, it.Content)
	}
	return strings.TrimRight(b.String(), "\n")
}
