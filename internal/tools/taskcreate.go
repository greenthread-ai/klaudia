package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
	"github.com/greenthread/klaudia/internal/tasks"
)

// TaskCreateInput is the TaskCreate tool's input.
type TaskCreateInput struct {
	Subject     string `json:"subject" jsonschema:"description=The task subject"`
	Description string `json:"description,omitempty" jsonschema:"description=The task description"`
	ActiveForm  string `json:"activeForm,omitempty" jsonschema:"description=Present-continuous form shown while in progress"`
}

// TaskCreate creates a task in the session task store.
type TaskCreate struct {
	schema *schema.Schema
	store  *tasks.Store
}

// NewTaskCreate constructs the TaskCreate tool backed by store.
func NewTaskCreate(store *tasks.Store) (*TaskCreate, error) {
	s, err := schema.For[TaskCreateInput]()
	if err != nil {
		return nil, fmt.Errorf("taskcreate: build schema: %w", err)
	}
	return &TaskCreate{schema: s, store: store}, nil
}

func (t *TaskCreate) Name() string { return "TaskCreate" }

func (t *TaskCreate) Description(context.Context) (string, error) {
	return "Create a task in the session task store. Provide subject (required), description, and activeForm.", nil
}

func (t *TaskCreate) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *TaskCreate) ValidateInput(raw json.RawMessage) error {
	return t.schema.Validate(raw)
}

// PermissionRequest: TaskCreate has no filesystem/exec side effects.
func (t *TaskCreate) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *TaskCreate) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *TaskCreate) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in TaskCreateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	task := t.store.Create(in.Subject, in.Description, in.ActiveForm)
	return []Result{{Content: fmt.Sprintf("Created %s: %s", task.ID, task.Subject)}}, nil
}
