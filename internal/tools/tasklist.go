package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
	"github.com/greenthread/klaudia/internal/tasks"
)

// TaskListInput is the TaskList tool's input.
type TaskListInput struct{}

// TaskList lists tasks in the session task store.
type TaskList struct {
	schema *schema.Schema
	store  *tasks.Store
}

// NewTaskList constructs the TaskList tool backed by store.
func NewTaskList(store *tasks.Store) (*TaskList, error) {
	s, err := schema.For[TaskListInput]()
	if err != nil {
		return nil, fmt.Errorf("tasklist: build schema: %w", err)
	}
	return &TaskList{schema: s, store: store}, nil
}

func (t *TaskList) Name() string { return "TaskList" }

func (t *TaskList) Description(context.Context) (string, error) {
	return "List tasks in the session task store. Returns each task as <id> [<status>] <subject>.", nil
}

func (t *TaskList) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *TaskList) ValidateInput(raw json.RawMessage) error {
	return t.schema.Validate(raw)
}

// PermissionRequest: TaskList has no filesystem/exec side effects.
func (t *TaskList) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *TaskList) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *TaskList) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in TaskListInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	tasks := t.store.List()
	if len(tasks) == 0 {
		return []Result{{Content: "No tasks."}}, nil
	}

	var b strings.Builder
	for _, task := range tasks {
		fmt.Fprintf(&b, "%s [%s] %s\n", task.ID, task.Status, task.Subject)
	}
	return []Result{{Content: strings.TrimRight(b.String(), "\n")}}, nil
}
