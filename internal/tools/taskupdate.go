package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
	"github.com/greenthread-ai/klaudia/internal/tasks"
)

// TaskUpdateInput is the TaskUpdate tool's input.
type TaskUpdateInput struct {
	ID          string `json:"id" jsonschema:"description=The task id"`
	Status      string `json:"status,omitempty" jsonschema:"description=One of: pending, in_progress, completed"`
	Subject     string `json:"subject,omitempty" jsonschema:"description=The task subject"`
	Description string `json:"description,omitempty" jsonschema:"description=The task description"`
	ActiveForm  string `json:"activeForm,omitempty" jsonschema:"description=Present-continuous form shown while in progress"`
}

// TaskUpdate updates a task in the session task store.
type TaskUpdate struct {
	schema *schema.Schema
	store  *tasks.Store
}

// NewTaskUpdate constructs the TaskUpdate tool backed by store.
func NewTaskUpdate(store *tasks.Store) (*TaskUpdate, error) {
	s, err := schema.For[TaskUpdateInput]()
	if err != nil {
		return nil, fmt.Errorf("taskupdate: build schema: %w", err)
	}
	return &TaskUpdate{schema: s, store: store}, nil
}

func (t *TaskUpdate) Name() string { return "TaskUpdate" }

func (t *TaskUpdate) Description(context.Context) (string, error) {
	return "Update a task in the session task store. Empty status, subject, description, and activeForm leave existing fields unchanged.", nil
}

func (t *TaskUpdate) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *TaskUpdate) ValidateInput(raw json.RawMessage) error {
	if err := t.schema.Validate(raw); err != nil {
		return err
	}
	var in TaskUpdateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	switch in.Status {
	case "", "pending", "in_progress", "completed":
		return nil
	default:
		return fmt.Errorf("status %q is invalid (want pending|in_progress|completed)", in.Status)
	}
}

// PermissionRequest: TaskUpdate has no filesystem/exec side effects.
func (t *TaskUpdate) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *TaskUpdate) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *TaskUpdate) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in TaskUpdateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	task, err := t.store.Update(in.ID, in.Status, in.Subject, in.Description, in.ActiveForm)
	if err != nil {
		return []Result{{Content: err.Error(), IsError: true}}, nil
	}
	return []Result{{Content: fmt.Sprintf("Updated %s: %s", task.ID, task.Subject)}}, nil
}
