package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
	"github.com/greenthread-ai/klaudia/internal/tasks"
)

// TaskGetInput is the TaskGet tool's input.
type TaskGetInput struct {
	ID string `json:"id" jsonschema:"description=The task id"`
}

// TaskGet gets one task from the session task store.
type TaskGet struct {
	schema *schema.Schema
	store  *tasks.Store
}

// NewTaskGet constructs the TaskGet tool backed by store.
func NewTaskGet(store *tasks.Store) (*TaskGet, error) {
	s, err := schema.For[TaskGetInput]()
	if err != nil {
		return nil, fmt.Errorf("taskget: build schema: %w", err)
	}
	return &TaskGet{schema: s, store: store}, nil
}

func (t *TaskGet) Name() string { return "TaskGet" }

func (t *TaskGet) Description(context.Context) (string, error) {
	return "Get details for a task from the session task store by id.", nil
}

func (t *TaskGet) InputSchema() json.RawMessage { return t.schema.Raw }

func (t *TaskGet) ValidateInput(raw json.RawMessage) error {
	return t.schema.Validate(raw)
}

// PermissionRequest: TaskGet has no filesystem/exec side effects.
func (t *TaskGet) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (t *TaskGet) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (t *TaskGet) Execute(_ context.Context, _ Context, raw json.RawMessage) ([]Result, error) {
	var in TaskGetInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	task, ok := t.store.Get(in.ID)
	if !ok {
		return []Result{{Content: fmt.Sprintf("Task %s not found", in.ID), IsError: true}}, nil
	}
	return []Result{{Content: renderTask(task)}}, nil
}

func renderTask(task tasks.Task) string {
	return fmt.Sprintf("ID: %s\nSubject: %s\nDescription: %s\nStatus: %s\nActiveForm: %s", task.ID, task.Subject, task.Description, task.Status, task.ActiveForm)
}
