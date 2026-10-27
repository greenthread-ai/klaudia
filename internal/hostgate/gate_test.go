package hostgate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
	"github.com/greenthread-ai/klaudia/pkg/agent"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

func gateFixture(t *testing.T) (*HostGate, string) {
	t.Helper()
	base := t.TempDir()
	home := filepath.Join(base, "home")
	proj := filepath.Join(base, "proj")
	for _, d := range []string{home, proj} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	roots := trust.NewRoots(home, proj)
	g := &HostGate{
		Roots:  func() trust.Roots { return roots },
		Ledger: trust.NewLedger(roots),
	}
	return g, proj
}

func bashInput(cmd string) []byte {
	b, _ := json.Marshal(map[string]string{"command": cmd})
	return b
}

func TestGateAllowsProjectWork(t *testing.T) {
	g, proj := gateFixture(t)
	for _, cmd := range []string{
		"go build ./...",
		"rm -rf ./dist",
		"cat /etc/os-release",
		"ssh staging sudo systemctl restart nginx",
	} {
		d := g.decide("Bash", bashInput(cmd), proj, true)
		if !d.Allow {
			t.Errorf("%q was gated: refuse=%q ask=%v", cmd, d.Refuse, d.Ask)
		}
	}
}

func TestGateStopsHostChanges(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	d := g.decide("Bash", bashInput("sudo systemctl restart nginx"), proj, true)
	if d.Allow {
		t.Fatal("a service restart was allowed")
	}
	if !strings.Contains(d.Refuse, "RequestHostChange") {
		t.Errorf("refusal does not say what to do next: %q", d.Refuse)
	}
	if !strings.Contains(d.Refuse, "nginx") {
		t.Errorf("refusal does not say what it stopped: %q", d.Refuse)
	}
}

// Without a declaration tool the gate has to fall back to asking the user
// directly, or a frontend that has not wired one up would refuse host changes
// forever with instructions it cannot follow.
func TestGateFallsBackToAskingWhenNoDeclarationTool(t *testing.T) {
	g, proj := gateFixture(t)
	d := g.decide("Bash", bashInput("sudo systemctl restart nginx"), proj, true)
	if d.Allow || d.Refuse != "" || len(d.Ask) == 0 {
		t.Fatalf("expected an ask, got allow=%v refuse=%q ask=%v", d.Allow, d.Refuse, d.Ask)
	}
}

func TestGateCoversDeclaredWork(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	if _, err := g.Ledger.Mint(trust.Request{
		Summary: "install and configure nginx", Services: []string{"nginx"}, Packages: []string{"nginx"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"sudo apt-get install -y nginx", "sudo systemctl restart nginx"} {
		if d := g.decide("Bash", bashInput(cmd), proj, true); !d.Allow {
			t.Errorf("%q was refused despite a grant: %q", cmd, d.Refuse)
		}
	}
	if d := g.decide("Bash", bashInput("sudo systemctl restart postgresql"), proj, true); d.Allow {
		t.Error("an undeclared service restart was allowed")
	}
}

// Write and Read reach the machine through their own doors, and a gate that
// only reads command lines would leave both of them open.
func TestGateCoversFileTools(t *testing.T) {
	g, proj := gateFixture(t)
	home := g.Roots().Home

	write, _ := json.Marshal(map[string]string{"file_path": "/etc/nginx/nginx.conf"})
	if d := g.decide("Write", write, proj, true); d.Allow {
		t.Error("Write to /etc was allowed")
	}
	inProject, _ := json.Marshal(map[string]string{"file_path": filepath.Join(proj, "main.go")})
	if d := g.decide("Write", inProject, proj, true); !d.Allow {
		t.Error("Write inside the project was gated")
	}

	key, _ := json.Marshal(map[string]string{"file_path": filepath.Join(home, ".ssh", "id_rsa")})
	if d := g.decide("Read", key, proj, true); d.Allow {
		t.Error("Read of a private key was allowed")
	}
	// Reading the operating system is ordinary debugging.
	osFile, _ := json.Marshal(map[string]string{"file_path": "/etc/hosts"})
	if d := g.decide("Read", osFile, proj, true); !d.Allow {
		t.Error("Read of /etc/hosts was gated")
	}
}

// A nil gate is the only inert case left. There is no "off" posture: a session
// that wants nothing checked selects bypassPermissions, which is tested where
// it is read — ahead of the gate, in dispatch — rather than inside it.
func TestNilGateIsInert(t *testing.T) {
	_, proj := gateFixture(t)
	var nilGate *HostGate
	if d := nilGate.decide("Bash", bashInput("sudo rm -rf /etc"), proj, true); !d.Allow {
		t.Error("a nil gate gated a call")
	}
}

// A gate nobody configured a posture on still classifies. This is the opposite
// of the old zero value, which read as off, and it is the point of removing
// the posture: there is no way to end up with a wired-up gate that silently
// checks nothing.
func TestZeroPostureGateStillEnforces(t *testing.T) {
	g, proj := gateFixture(t)
	if d := g.decide("Bash", bashInput("sudo systemctl restart nginx"), proj, true); d.Allow {
		t.Error("a gate with no posture set allowed a host change")
	}
}

// The gate has to run before rule matching, or an allow rule launders a command
// line: autonomous allows every command on its own, so if permission.Check ran
// first — or instead — "sudo systemctl restart nginx" would simply go through.
// The gate is the thing that stops it, and it does not consult the mode.
func TestGateRunsBeforeThePermissionCheck(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	reg, bash := testRegistry(t)
	l := newLoop(reg)

	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo systemctl restart nginx")),
	}
	res := dispatch(t, l, tu, agent.Options{
		WorkingDir: proj,
		Gate:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
	}, nil)

	body := resultText(res)
	if !strings.Contains(body, "RequestHostChange") {
		t.Fatalf("autonomous laundered a host change; result was %q", body)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("the command ran anyway: %v", bash.ran)
	}
}

// bypassPermissions means no checks. Honouring half of its contract would be
// worse than honouring none of it — the user asked for an ungated session.
func TestBypassSkipsTheGate(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	reg, bash := testRegistry(t)
	l := newLoop(reg)
	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo systemctl restart nginx")),
	}
	dispatch(t, l, tu, agent.Options{
		WorkingDir: proj,
		Gate:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)},
	}, nil)
	if len(bash.ran) != 1 {
		t.Fatalf("bypassPermissions did not run the command: %v", bash.ran)
	}
}

// Approving at the point of use mints a grant, so the next step of the same
// operation proceeds. Without this, approving the install would still stop at
// the restart, which is the fatigue the whole design exists to remove.
func TestApprovalMintsAGrant(t *testing.T) {
	g, proj := gateFixture(t)
	reg, bash := testRegistry(t)
	l := newLoop(reg)
	asked := 0
	opts := agent.Options{
		WorkingDir: proj,
		Gate:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver: agent.ApproverFunc(func(ctx context.Context, req agent.ApprovalRequest) permission.Decision {
			asked++
			if CardOf(req) == nil {
				t.Error("approval request carried no HostChange card")
			}
			return permission.Decision{Behavior: permission.Allow}
		}),
	}
	run := func(cmd string) {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(bashInput(cmd))}
		dispatch(t, l, tu, opts, nil)
	}
	run("sudo systemctl restart nginx")
	run("sudo systemctl stop nginx")
	if asked != 1 {
		t.Fatalf("asked %d times; one approval should have covered both", asked)
	}
	if len(bash.ran) != 2 {
		t.Fatalf("both approved steps should have run, ran %v", bash.ran)
	}
}

func TestDeclinedHostChangeFailsOnlyThatCall(t *testing.T) {
	g, proj := gateFixture(t)
	reg, bash := testRegistry(t)
	l := newLoop(reg)
	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo reboot")),
	}
	res := dispatch(t, l, tu, agent.Options{
		WorkingDir: proj,
		Gate:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver: agent.ApproverFunc(func(ctx context.Context, req agent.ApprovalRequest) permission.Decision {
			return permission.Decision{Behavior: permission.Deny}
		}),
	}, nil)

	body := resultText(res)
	if !strings.Contains(body, "declined") {
		t.Fatalf("result did not say the user declined: %q", body)
	}
	// It must tell the model to carry on, not to abort: work already done stands.
	if !strings.Contains(body, "Continue with the rest of the task") {
		t.Errorf("refusal reads as an abort rather than a skip: %q", body)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("a declined change ran anyway: %v", bash.ran)
	}
}

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
