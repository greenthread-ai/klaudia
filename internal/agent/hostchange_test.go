package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/internal/trust"
)

// The scenario the whole design exists for, end to end through dispatch:
// declare the operation once, then every step inside it runs untouched.
func TestDeclareOnceThenSilence(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"

	declare, err := tools.NewRequestHostChange()
	if err != nil {
		t.Fatal(err)
	}
	reg, bash := testRegistry(t, declare)
	l := New(nil, reg)

	asked := 0
	var card *HostChange
	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeDefault)},
		Approver: ApproverFunc(func(ctx context.Context, req ApprovalRequest) permission.Decision {
			asked++
			card = req.HostChange
			return permission.Decision{Behavior: permission.Allow}
		}),
	}
	run := func(name string, input any) string {
		raw, _ := json.Marshal(input)
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: name, Input: json.RawMessage(raw)}
		return resultText(l.dispatch(context.Background(), tu, opts, nil, nil,
			map[string]int{}, map[string]errStreak{}))
	}

	out := run("RequestHostChange", map[string]any{
		"summary":  "Install nginx and configure it as a development proxy",
		"reason":   "the task asks for the app to run behind a local proxy",
		"paths":    []string{"/etc/nginx/nginx.conf"},
		"services": []string{"nginx"},
		"packages": []string{"nginx"},
	})
	if !strings.Contains(out, "Approved") {
		t.Fatalf("declaration was not approved: %s", out)
	}
	if asked != 1 {
		t.Fatalf("asked %d times for one declaration", asked)
	}
	if card == nil || !card.Declared {
		t.Fatalf("approval card was not marked as declared: %+v", card)
	}
	if card.Reason == "" {
		t.Error("the card carried no reason — the user is deciding whether it is worth it")
	}

	for _, cmd := range []string{
		"sudo apt-get install -y nginx",
		"sudo mkdir -p /etc/nginx/conf.d",
		"sudo tee /etc/nginx/conf.d/app.conf",
		"sudo nginx -t",
		"sudo systemctl restart nginx",
	} {
		if body := run("Bash", map[string]string{"command": cmd}); strings.Contains(body, "RequestHostChange") {
			t.Errorf("%q was stopped despite the declaration: %s", cmd, body)
		}
	}
	if asked != 1 {
		t.Fatalf("asked %d times; the declaration should have covered every step", asked)
	}
	if len(bash.ran) != 5 {
		t.Fatalf("ran %v", bash.ran)
	}
}

// Scope drift: the model goes outside what was approved and is told to declare
// the extra part, with the user seeing that it was not in the original.
func TestDriftAsksAgainAndSaysSo(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	declare, _ := tools.NewRequestHostChange()
	reg, _ := testRegistry(t, declare)
	l := New(nil, reg)

	var cards []*HostChange
	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeDefault)},
		Approver: ApproverFunc(func(ctx context.Context, req ApprovalRequest) permission.Decision {
			cards = append(cards, req.HostChange)
			return permission.Decision{Behavior: permission.Allow}
		}),
	}
	run := func(name string, input any) string {
		raw, _ := json.Marshal(input)
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: name, Input: json.RawMessage(raw)}
		return resultText(l.dispatch(context.Background(), tu, opts, nil, nil,
			map[string]int{}, map[string]errStreak{}))
	}

	run("RequestHostChange", map[string]any{
		"summary": "Configure nginx", "reason": "local proxy",
		"services": []string{"nginx"},
	})
	body := run("Bash", map[string]string{"command": "sudo systemctl restart postgresql"})
	if !strings.Contains(body, "RequestHostChange") {
		t.Fatalf("an unapproved service was not stopped: %s", body)
	}
	if !strings.Contains(body, "outside the scope") {
		t.Errorf("the refusal did not say it was drift: %s", body)
	}

	run("RequestHostChange", map[string]any{
		"summary": "Restart postgres too", "reason": "the app needs the database up",
		"services": []string{"postgresql"},
	})
	if len(cards) != 2 || !cards[1].Drift {
		t.Fatalf("the second card was not marked as drift: %+v", cards)
	}
}

// Declining fails that one tool call. Prior work stands and the model is told
// to carry on, because the alternative throws away completed work for no reason.
func TestDeclinedDeclarationDoesNotMintAGrant(t *testing.T) {
	g, proj := gateFixture(t)
	g.DeclareTool = "RequestHostChange"
	declare, _ := tools.NewRequestHostChange()
	reg, _ := testRegistry(t, declare)
	l := New(nil, reg)

	opts := Options{
		WorkingDir: proj,
		Host:       g,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeDefault)},
		Approver: ApproverFunc(func(ctx context.Context, req ApprovalRequest) permission.Decision {
			return permission.Decision{Behavior: permission.Deny}
		}),
	}
	raw, _ := json.Marshal(map[string]any{
		"summary": "Install nginx", "reason": "proxy", "packages": []string{"nginx"},
	})
	body := resultText(l.dispatch(context.Background(),
		anthropic.BetaToolUseBlock{ID: "t", Name: "RequestHostChange", Input: json.RawMessage(raw)},
		opts, nil, nil, map[string]int{}, map[string]errStreak{}))

	if !strings.Contains(body, "declined") {
		t.Fatalf("result did not report the decline: %s", body)
	}
	if !strings.Contains(body, "Continue with the rest of the task") {
		t.Errorf("the refusal reads as an abort rather than a skip: %s", body)
	}
	if n := len(g.Ledger.List()); n != 0 {
		t.Fatalf("a declined declaration minted %d grants", n)
	}
}

// An over-broad ask is refused before it reaches the user. Whether Klaudia gets
// all of /etc should not depend on how tired someone is at the time.
func TestOverBroadDeclarationNeverReachesTheUser(t *testing.T) {
	g, _ := gateFixture(t)
	asked := 0
	h := hostChangeApprover{
		gate: g,
		approver: ApproverFunc(func(ctx context.Context, req ApprovalRequest) permission.Decision {
			asked++
			return permission.Decision{Behavior: permission.Allow}
		}),
	}
	out, err := h.RequestHostChange(context.Background(), tools.HostChangeRequest{
		Summary: "reconfigure the system", Reason: "because", Paths: []string{"/etc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Approved {
		t.Error("a request for all of /etc was approved")
	}
	if asked != 0 {
		t.Error("an over-broad request was put to the user")
	}
	if !strings.Contains(out.Message, "Narrow the request") {
		t.Errorf("the model was not told how to fix it: %q", out.Message)
	}
}

// Validation is the model's first line of feedback, so it has to be specific
// enough to act on.
func TestDeclarationValidation(t *testing.T) {
	declare, err := tools.NewRequestHostChange()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		input map[string]any
		want  string
	}{
		{"no scope", map[string]any{"summary": "do a thing", "reason": "because"}, "at least one path"},
		{"wildcard", map[string]any{
			"summary": "s", "reason": "r", "paths": []string{"/etc/nginx/*.conf"},
		}, "pattern"},
		// The schema marks reason required, so this is caught there; the tool's
		// own check covers a reason that is present but blank.
		{"no reason", map[string]any{"summary": "s", "packages": []string{"nginx"}}, "reason"},
		{"blank reason", map[string]any{
			"summary": "s", "reason": "   ", "packages": []string{"nginx"},
		}, "reason is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.input)
			err := declare.ValidateInput(raw)
			if err == nil {
				t.Fatalf("accepted %v", tc.input)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// With no one to ask, the tool says so. A model told "approved" here would then
// be refused by the gate on every command, which is a confusing way to find out
// there is no user.
func TestNoApproverSaysSo(t *testing.T) {
	declare, _ := tools.NewRequestHostChange()
	raw, _ := json.Marshal(map[string]any{
		"summary": "s", "reason": "r", "packages": []string{"nginx"},
	})
	res, err := declare.Execute(context.Background(), tools.Context{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || !res[0].IsError {
		t.Fatalf("expected an error result, got %+v", res)
	}
	if !strings.Contains(res[0].Content, "no one to ask") {
		t.Errorf("content = %q", res[0].Content)
	}
}

// The tool must not be usable to make a change, only to ask about one.
func TestDeclarationToolCannotActOnItsOwn(t *testing.T) {
	g, _ := gateFixture(t)
	if _, err := g.Ledger.Mint(trust.Request{
		Summary: "x", Services: []string{"nginx"},
	}); err != nil {
		t.Fatal(err)
	}
	// A grant exists, but the tool itself still has no path to execution: its
	// Execute only calls the approver. Guard the shape rather than the wiring.
	declare, _ := tools.NewRequestHostChange()
	if req := declare.PermissionRequest(nil); req.Specifier != "" {
		t.Errorf("the declaration tool carries a rule specifier %q, so a rule could pre-approve asking",
			req.Specifier)
	}
}
