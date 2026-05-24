package agent

import (
	"context"
	"testing"

	"github.com/greenthread/klaudia/internal/permission"
)

func TestDenyAllDenies(t *testing.T) {
	d := DenyAll.Approve(context.Background(), ApprovalRequest{ToolName: "Bash", Suggestion: "needs ok"})
	if d.Behavior != permission.Deny {
		t.Errorf("behavior = %q, want deny", d.Behavior)
	}
	if d.Message != "needs ok" {
		t.Errorf("message = %q, want the suggestion passed through", d.Message)
	}
}

func TestApproverFuncAllows(t *testing.T) {
	var gotName string
	allow := ApproverFunc(func(_ context.Context, req ApprovalRequest) permission.Decision {
		gotName = req.ToolName
		return permission.Decision{Behavior: permission.Allow}
	})
	d := allow.Approve(context.Background(), ApprovalRequest{ToolName: "Edit"})
	if d.Behavior != permission.Allow {
		t.Errorf("behavior = %q, want allow", d.Behavior)
	}
	if gotName != "Edit" {
		t.Errorf("approver saw tool %q, want Edit", gotName)
	}
}
