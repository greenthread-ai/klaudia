package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
	"github.com/greenthread-ai/klaudia/pkg/schema"
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
	store  *plan.Store
}

// NewExitPlanMode constructs the tool. store receives the approved plan, which
// is the point at which it becomes a durable artefact.
func NewExitPlanMode(store *plan.Store) (*ExitPlanMode, error) {
	s, err := schema.For[ExitPlanModeInput]()
	if err != nil {
		return nil, fmt.Errorf("exitplanmode: build schema: %w", err)
	}
	if store == nil {
		// An in-memory store rather than a nil check at every use. Approval
		// still works, it just leaves no file.
		store = plan.New("")
	}
	return &ExitPlanMode{schema: s, store: store}, nil
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
	return []Result{{Content: e.approved(planFor(tctx, e.store), in.Plan)}}, nil
}

// approved builds the model-facing result for an accepted plan.
//
// It used to be the bare sentence "Plan approved. Plan mode is now off —
// proceed with the implementation", and the plan itself survived only as prose
// in the message history, where compaction could summarise it away. So the plan
// is persisted here, and the result tells the model to put the steps somewhere
// both it and the user can see them — which is the behaviour the user asked
// for: approving a plan should start working through a visible list, not just
// unlock the tools.
func (e *ExitPlanMode) approved(store *plan.Store, prose string) string {
	msg := "Plan approved. Plan mode is now off — proceed with the implementation.\n\n" +
		"Record the plan's steps with TodoWrite now, before starting the first one, and keep " +
		"it updated as you go: mark exactly one item in_progress, complete it, then move on. " +
		"Revise the list when you hit an error, a new requirement or an unexpected dependency — " +
		"the list is what the user is watching to see what is done and what is left."
	if err := store.Approve(prose); err != nil {
		// Worth telling the model: the steps it is about to record will stay in
		// memory only, so a compaction or a resume loses them.
		return msg + "\n\n(The plan could not be saved: " + err.Error() + ")"
	}
	if path := store.Path(); path != "" {
		msg += "\n\nThe approved plan is saved at " + path +
			", and TodoWrite keeps its checklist in step with your list."
	}
	return msg
}
