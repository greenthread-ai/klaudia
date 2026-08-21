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

// Unattended runs should do the work. Denying ordinary project work because no
// human is watching makes automation useless, which is what DenyAll did for
// everything.
func TestHeadlessRunsProjectWork(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	reg, bash := testRegistry(t)
	l := New(nil, reg)

	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver:   HeadlessApprover(false),
	}
	for _, cmd := range []string{"go build ./...", "rm -rf ./dist", "git commit -m wip"} {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		l.dispatch(context.Background(),
			anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(raw)},
			opts, nil, nil, map[string]int{}, map[string]errStreak{})
	}
	if len(bash.ran) != 3 {
		t.Fatalf("headless refused ordinary work: ran %v", bash.ran)
	}
}

// A host change with no one to ask is refused, and the refusal names the flag.
// Failing without saying what would have worked is how an automation run wastes
// someone's afternoon.
func TestHeadlessRefusesHostChangesAndNamesTheFlag(t *testing.T) {
	g, proj := gateFixture(t)
	declare, err := tools.NewRequestHostChange()
	if err != nil {
		t.Fatal(err)
	}
	g.DeclareTool = "RequestHostChange"
	reg, bash := testRegistry(t, declare)
	l := New(nil, reg)

	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver:   HeadlessApprover(false),
	}
	raw, _ := json.Marshal(map[string]any{
		"summary": "Install nginx", "reason": "the app runs behind a proxy",
		"packages": []string{"nginx"},
	})
	body := resultText(l.dispatch(context.Background(),
		anthropic.BetaToolUseBlock{ID: "t", Name: "RequestHostChange", Input: json.RawMessage(raw)},
		opts, nil, nil, map[string]int{}, map[string]errStreak{}))

	if !strings.Contains(body, "--allow-host-changes") {
		t.Errorf("the refusal does not name the flag that would permit it: %s", body)
	}
	if n := len(g.Ledger.List()); n != 0 {
		t.Fatalf("a headless refusal minted %d grants", n)
	}

	// And the change itself still cannot happen.
	cmdRaw, _ := json.Marshal(map[string]string{"command": "sudo apt-get install -y nginx"})
	l.dispatch(context.Background(),
		anthropic.BetaToolUseBlock{ID: "t2", Name: "Bash", Input: json.RawMessage(cmdRaw)},
		opts, nil, nil, map[string]int{}, map[string]errStreak{})
	if len(bash.ran) != 0 {
		t.Fatalf("the host change ran anyway: %v", bash.ran)
	}
}

// --allow-host-changes is the deliberate opt-in for a machine the operator is
// willing to have reconfigured.
func TestAllowHostChangesPermitsThem(t *testing.T) {
	g, proj := gateFixture(t)
	declare, _ := tools.NewRequestHostChange()
	g.DeclareTool = "RequestHostChange"
	reg, bash := testRegistry(t, declare)
	l := New(nil, reg)

	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver:   HeadlessApprover(true),
	}
	raw, _ := json.Marshal(map[string]any{
		"summary": "Install nginx", "reason": "the app runs behind a proxy",
		"packages": []string{"nginx"},
	})
	body := resultText(l.dispatch(context.Background(),
		anthropic.BetaToolUseBlock{ID: "t", Name: "RequestHostChange", Input: json.RawMessage(raw)},
		opts, nil, nil, map[string]int{}, map[string]errStreak{}))
	if !strings.Contains(body, "Approved") {
		t.Fatalf("--allow-host-changes did not approve: %s", body)
	}

	cmdRaw, _ := json.Marshal(map[string]string{"command": "sudo apt-get install -y nginx"})
	l.dispatch(context.Background(),
		anthropic.BetaToolUseBlock{ID: "t2", Name: "Bash", Input: json.RawMessage(cmdRaw)},
		opts, nil, nil, map[string]int{}, map[string]errStreak{})
	if len(bash.ran) != 1 {
		t.Fatalf("the approved install did not run: %v", bash.ran)
	}
}

// The flag permits the declared operation, not everything. An automation run
// that asked to install nginx has not asked to reboot.
func TestAllowHostChangesStillScopesToTheDeclaration(t *testing.T) {
	g, proj := gateFixture(t)
	declare, _ := tools.NewRequestHostChange()
	g.DeclareTool = "RequestHostChange"
	reg, bash := testRegistry(t, declare)
	l := New(nil, reg)

	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver:   HeadlessApprover(true),
	}
	raw, _ := json.Marshal(map[string]any{
		"summary": "Install nginx", "reason": "proxy", "packages": []string{"nginx"},
	})
	l.dispatch(context.Background(),
		anthropic.BetaToolUseBlock{ID: "t", Name: "RequestHostChange", Input: json.RawMessage(raw)},
		opts, nil, nil, map[string]int{}, map[string]errStreak{})

	cmdRaw, _ := json.Marshal(map[string]string{"command": "sudo reboot"})
	body := resultText(l.dispatch(context.Background(),
		anthropic.BetaToolUseBlock{ID: "t2", Name: "Bash", Input: json.RawMessage(cmdRaw)},
		opts, nil, nil, map[string]int{}, map[string]errStreak{}))
	if !strings.Contains(body, "RequestHostChange") {
		t.Errorf("an undeclared reboot was not stopped: %s", body)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("the reboot ran: %v", bash.ran)
	}
}
