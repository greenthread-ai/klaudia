package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
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
	return &HostGate{
		Policy: HostEnforce,
		Roots:  func() trust.Roots { return roots },
		Ledger: trust.NewLedger(roots),
	}, proj
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
		d := g.Check("Bash", bashInput(cmd), proj)
		if !d.Allow {
			t.Errorf("%q was gated: refuse=%q ask=%v", cmd, d.Refuse, d.Ask)
		}
	}
}

func TestGateStopsHostChanges(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	d := g.Check("Bash", bashInput("sudo systemctl restart nginx"), proj)
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
	d := g.Check("Bash", bashInput("sudo systemctl restart nginx"), proj)
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
		if d := g.Check("Bash", bashInput(cmd), proj); !d.Allow {
			t.Errorf("%q was refused despite a grant: %q", cmd, d.Refuse)
		}
	}
	if d := g.Check("Bash", bashInput("sudo systemctl restart postgresql"), proj); d.Allow {
		t.Error("an undeclared service restart was allowed")
	}
}

// Write and Read reach the machine through their own doors, and a gate that
// only reads command lines would leave both of them open.
func TestGateCoversFileTools(t *testing.T) {
	g, proj := gateFixture(t)
	home := g.Roots().Home

	write, _ := json.Marshal(map[string]string{"file_path": "/etc/nginx/nginx.conf"})
	if d := g.Check("Write", write, proj); d.Allow {
		t.Error("Write to /etc was allowed")
	}
	inProject, _ := json.Marshal(map[string]string{"file_path": filepath.Join(proj, "main.go")})
	if d := g.Check("Write", inProject, proj); !d.Allow {
		t.Error("Write inside the project was gated")
	}

	key, _ := json.Marshal(map[string]string{"file_path": filepath.Join(home, ".ssh", "id_rsa")})
	if d := g.Check("Read", key, proj); d.Allow {
		t.Error("Read of a private key was allowed")
	}
	// Reading the operating system is ordinary debugging.
	osFile, _ := json.Marshal(map[string]string{"file_path": "/etc/hosts"})
	if d := g.Check("Read", osFile, proj); !d.Allow {
		t.Error("Read of /etc/hosts was gated")
	}
}

func TestObserveModeChangesNothing(t *testing.T) {
	g, proj := gateFixture(t)
	g.Policy = HostObserve
	var seen []HostReport
	g.Observed = func(r HostReport) { seen = append(seen, r) }

	if d := g.Check("Bash", bashInput("sudo systemctl restart nginx"), proj); !d.Allow {
		t.Fatal("observe mode refused a call")
	}
	if len(seen) != 1 || seen[0].Enforced {
		t.Fatalf("expected one non-enforced report, got %+v", seen)
	}
	if !strings.Contains(seen[0].Summary, "nginx") {
		t.Errorf("report summary = %q", seen[0].Summary)
	}
}

func TestHostOffAndNilGateAreInert(t *testing.T) {
	g, proj := gateFixture(t)
	g.Policy = HostOff
	if d := g.Check("Bash", bashInput("sudo rm -rf /etc"), proj); !d.Allow {
		t.Error("HostOff gated a call")
	}
	var nilGate *HostGate
	if d := nilGate.Check("Bash", bashInput("sudo rm -rf /etc"), proj); !d.Allow {
		t.Error("a nil gate gated a call")
	}
}

func TestResolveHostPolicy(t *testing.T) {
	for _, tc := range []struct {
		configured string
		legacy     bool
		want       HostPolicy
		notice     bool
	}{
		{"", false, HostEnforce, false},
		{"", true, HostObserve, true}, // existing rules: observe, and say so
		{"enforce", true, HostEnforce, false},
		{"observe", false, HostObserve, false},
		{"off", true, HostOff, false},
		{"nonsense", false, HostEnforce, false},
		{"nonsense", true, HostObserve, true},
	} {
		got, notice := ResolveHostPolicy(tc.configured, tc.legacy)
		if got != tc.want || notice != tc.notice {
			t.Errorf("ResolveHostPolicy(%q, %v) = %v,%v want %v,%v",
				tc.configured, tc.legacy, got, notice, tc.want, tc.notice)
		}
	}
}

// The gate has to run before rule matching, or an allow rule launders a command
// line: "Bash(sudo:*)" would otherwise authorise anything starting with sudo.
func TestGateRunsBeforeAllowRules(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	reg, bash := testRegistry(t)
	l := New(nil, reg)

	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo systemctl restart nginx")),
	}
	allow, err := permission.ParseRules([]string{"Bash(sudo:*)"})
	if err != nil {
		t.Fatal(err)
	}
	res := l.dispatch(context.Background(), tu, Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeDefault), Allow: allow},
	}, nil, nil, map[string]int{}, map[string]errStreak{})

	body := resultText(res)
	if !strings.Contains(body, "RequestHostChange") {
		t.Fatalf("an allow rule laundered a host change; result was %q", body)
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
	l := New(nil, reg)
	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo systemctl restart nginx")),
	}
	l.dispatch(context.Background(), tu, Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)},
	}, nil, nil, map[string]int{}, map[string]errStreak{})
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
	l := New(nil, reg)
	asked := 0
	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeDefault)},
		Approver: ApproverFunc(func(ctx context.Context, req ApprovalRequest) permission.Decision {
			asked++
			if req.HostChange == nil {
				t.Error("approval request carried no HostChange card")
			}
			return permission.Decision{Behavior: permission.Allow}
		}),
	}
	run := func(cmd string) {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(bashInput(cmd))}
		l.dispatch(context.Background(), tu, opts, nil, nil, map[string]int{}, map[string]errStreak{})
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
	l := New(nil, reg)
	tu := anthropic.BetaToolUseBlock{
		ID: "t1", Name: "Bash", Input: json.RawMessage(bashInput("sudo reboot")),
	}
	res := l.dispatch(context.Background(), tu, Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeDefault)},
		Approver: ApproverFunc(func(ctx context.Context, req ApprovalRequest) permission.Decision {
			return permission.Decision{Behavior: permission.Deny}
		}),
	}, nil, nil, map[string]int{}, map[string]errStreak{})

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
type stubBash struct{ ran []string }

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
