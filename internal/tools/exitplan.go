package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread/klaudia/internal/permission"
	"github.com/greenthread/klaudia/internal/schema"
)

// ExitPlanModeInput is the ExitPlanMode tool's input.
type ExitPlanModeInput struct {
	Plan string `json:"plan" jsonschema:"description=The implementation plan to present to the user for approval, in markdown"`
}

// ExitPlanMode is called by the model after planning (in read-only plan mode) to
// present its plan and request approval to start implementing. On approval the
// frontend switches the session out of plan mode.
type ExitPlanMode struct {
	schema *schema.Schema
}

// NewExitPlanMode constructs the tool.
func NewExitPlanMode() (*ExitPlanMode, error) {
	s, err := schema.For[ExitPlanModeInput]()
	if err != nil {
		return nil, fmt.Errorf("exitplanmode: build schema: %w", err)
	}
	return &ExitPlanMode{schema: s}, nil
}

func (e *ExitPlanMode) Name() string { return "ExitPlanMode" }

func (e *ExitPlanMode) Description(context.Context) (string, error) {
	return "Use this when you are in plan mode and have finished planning: present your " +
		"implementation plan to the user for approval. Only call it once the plan is complete. " +
		"If approved, plan mode ends and you may begin implementing.", nil
}

func (e *ExitPlanMode) InputSchema() json.RawMessage { return e.schema.Raw }

func (e *ExitPlanMode) ValidateInput(raw json.RawMessage) error { return e.schema.Validate(raw) }

// PermissionRequest: presenting a plan is harmless; always allowed.
func (e *ExitPlanMode) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

func (e *ExitPlanMode) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (e *ExitPlanMode) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in ExitPlanModeInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	if tctx.Plan == nil {
		// No interactive frontend to approve (headless): surface the plan and
		// let the caller decide out-of-band.
		return []Result{{Content: "Plan recorded (no interactive approver available):\n\n" + in.Plan}}, nil
	}
	approved, err := tctx.Plan.ExitPlan(ctx, in.Plan)
	if err != nil {
		return []Result{{Content: "Plan approval failed: " + err.Error(), IsError: true}}, nil
	}
	if !approved {
		return []Result{{Content: "The user did not approve the plan. Keep refining it in plan mode; do not modify anything yet."}}, nil
	}
	return []Result{{Content: "Plan approved. Plan mode is now off — proceed with the implementation."}}, nil
}
