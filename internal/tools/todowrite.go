package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
	"github.com/greenthread-ai/klaudia/pkg/schema"
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

// TodoWrite replaces the session plan's step list, mirroring the JS TodoWrite
// tool. The store is owned by the caller rather than built here: a list nobody
// outside the registry can read is a list no frontend can show, which is how
// this one went unrendered everywhere but ACP.
type TodoWrite struct {
	schema *schema.Schema
	store  *plan.Store
}

// NewTodoWrite constructs the TodoWrite tool backed by store.
func NewTodoWrite(store *plan.Store) (*TodoWrite, error) {
	s, err := schema.For[TodoInput]()
	if err != nil {
		return nil, fmt.Errorf("todowrite: build schema: %w", err)
	}
	return &TodoWrite{schema: s, store: store}, nil
}

// planFor is the store a plan tool writes to: the conversation's, when the
// loop supplied one (see Context.Plans), else the one the tool was built
// with — which is the right answer only for a process with one conversation.
func planFor(tctx Context, built *plan.Store) *plan.Store {
	if tctx.Plans != nil {
		return tctx.Plans
	}
	return built
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
		if !plan.ValidStatus(it.Status) {
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

func (t *TodoWrite) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in TodoInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	items := make([]plan.Item, 0, len(in.Todos))
	for _, it := range in.Todos {
		items = append(items, plan.Item{Content: it.Content, Status: it.Status, ActiveForm: it.ActiveForm})
	}
	// A write failure is reported to the model but does not fail the call: the
	// list is updated in memory either way, and losing the plan because
	// .klaudia is read-only would be a worse outcome than a stale file.
	note := ""
	if err := planFor(tctx, t.store).Replace(items); err != nil {
		note = "\n\n(The plan file could not be updated: " + err.Error() + ")"
	}
	return []Result{{Content: renderTodos(in.Todos) + note}}, nil
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
