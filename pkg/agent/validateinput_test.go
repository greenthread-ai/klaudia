package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// invalidInputBash stands in for a tool whose arguments are malformed: its
// ValidateInput always fails. Its CheckPermissions asks, so a recording
// approver fires if the permission step is reached. Either the gate or the
// approver firing would mean the call reached the gate or the user before
// being rejected as invalid.
type invalidInputBash struct{}

func (invalidInputBash) Name() string { return "Bash" }
func (invalidInputBash) Description(context.Context) (string, error) {
	return "run a shell command", nil
}
func (invalidInputBash) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
}
func (invalidInputBash) ValidateInput(json.RawMessage) error {
	return errors.New("command is required")
}
func (invalidInputBash) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.Command}
}
func (invalidInputBash) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Ask}
}
func (invalidInputBash) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	return []tools.Result{{Content: "(ran)"}}, nil
}

// recordingGate allows everything and records that it was consulted.
type recordingGate struct{ checked *bool }

func (g recordingGate) Check(context.Context, GateCall) GateVerdict {
	*g.checked = true
	return GateVerdict{Allow: true}
}
func (recordingGate) WouldAsk(string, []byte, string) bool    { return false }
func (recordingGate) HostChanges(Approver) tools.HostApprover { return nil }

// A tool_use with invalid arguments must be rejected on validation alone,
// before the host gate classifies it and before the user is asked to approve
// it. Approving (or gating) a call that can never run is the fatigue this
// reorder removes.
func TestInvalidInputSkipsGateAndApproval(t *testing.T) {
	gateChecked, approverCalled := false, false
	l := New(nil, tools.NewRegistry(invalidInputBash{}))
	opts := Options{
		WorkingDir: t.TempDir(),
		Gate:       recordingGate{checked: &gateChecked},
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver: ApproverFunc(func(context.Context, ApprovalRequest) permission.Decision {
			approverCalled = true
			return permission.Decision{Behavior: permission.Allow}
		}),
	}

	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo systemctl restart nginx")),
	}
	body := resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, newFailureState()))

	if !strings.Contains(body, "Input validation error") {
		t.Fatalf("expected an input validation error, got %q", body)
	}
	// The helpful field list must survive the move.
	if !strings.Contains(body, "accepts") {
		t.Errorf("validation error dropped the accepted-field list: %q", body)
	}
	if gateChecked {
		t.Error("the host gate processed an invalid tool call")
	}
	if approverCalled {
		t.Error("the user was asked to approve an invalid tool call")
	}
}

// The failure counter must still bump on an invalid call so the loop-breaker
// behaves as before — only the timing of validation changed, not its effect.
func TestInvalidInputStillCountsAsAFailure(t *testing.T) {
	gateChecked := false
	l := New(nil, tools.NewRegistry(invalidInputBash{}))
	opts := Options{
		WorkingDir: t.TempDir(),
		Gate:       recordingGate{checked: &gateChecked},
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
	}
	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo reboot")),
	}
	fs := newFailureState()
	l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs)

	key := "Bash\x00" + string(bashInput("sudo reboot"))
	if n := fs.count(key); n != 1 {
		t.Fatalf("invalid input did not bump the failure counter: %d (%v)", n, fs.failures)
	}
}
