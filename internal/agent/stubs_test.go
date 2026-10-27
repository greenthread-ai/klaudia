package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// stubBash stands in for the real Bash tool in dispatch tests.
//
// The real one would run the command. These tests classify things like
// `sudo systemctl restart nginx`, and a test that reaches Execute would either
// hang on a password prompt or, worse, work. The stub records what it was asked
// to run so a test can assert the gate stopped it.
type stubBash struct {
	ran []string
	// err, when set, makes every execution fail with this message — for the
	// loop-breaker tests, which need a tool that fails identically.
	err string
}

func (s *stubBash) Name() string { return "Bash" }
func (s *stubBash) Description(context.Context) (string, error) {
	return "run a shell command", nil
}
func (s *stubBash) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`)
}
func (s *stubBash) ValidateInput(json.RawMessage) error { return nil }
func (s *stubBash) PermissionRequest(raw json.RawMessage) permission.PermissionRequest {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &in)
	return permission.PermissionRequest{Specifier: in.Command}
}
func (s *stubBash) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (s *stubBash) Execute(_ context.Context, _ tools.Context, raw json.RawMessage) ([]tools.Result, error) {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &in)
	if s.err != "" {
		return []tools.Result{{Content: s.err, IsError: true}}, nil
	}
	s.ran = append(s.ran, in.Command)
	return []tools.Result{{Content: "(stub)"}}, nil
}

func testRegistry(t *testing.T, extra ...tools.Tool) (*tools.Registry, *stubBash) {
	t.Helper()
	b := &stubBash{}
	return tools.NewRegistry(append([]tools.Tool{b}, extra...)...), b
}

func resultText(block anthropic.BetaContentBlockParamUnion) string {
	b, _ := json.Marshal(block)
	return string(b)
}

func bashInput(cmd string) []byte {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

// refusingGate refuses any call whose input contains match, with one fixed
// message — the loop's view of a host gate, without the policy behind it.
type refusingGate struct{ match, msg string }

func (g refusingGate) Check(_ context.Context, call GateCall) GateVerdict {
	if strings.Contains(string(call.Input), g.match) {
		return GateVerdict{Refuse: g.msg}
	}
	return GateVerdict{Allow: true}
}
func (refusingGate) WouldAsk(string, []byte, string) bool    { return false }
func (refusingGate) HostChanges(Approver) tools.HostApprover { return nil }
