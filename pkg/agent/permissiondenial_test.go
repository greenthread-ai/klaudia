package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// planModeBash is stubBash carrying the real plan-mode rule: a mode-dependent
// denial whose message is byte-identical whatever the command was.
//
// That identical text is the entire mechanism of the bug these tests cover, so
// the stub reproduces it rather than mentioning the command — a denial that
// named its command would give every call a distinct signature and the streak
// would never see "same error, varied inputs" at all.
type planModeBash struct{ *stubBash }

func (p planModeBash) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	if permission.CurrentMode(pctx) == permission.ModePlan {
		return permission.Decision{
			Behavior: permission.Deny,
			Message:  "plan mode is read-only; command execution is not allowed",
		}
	}
	return permission.Decision{Behavior: permission.Allow}
}

// askingBash always defers to the frontend's Approver, so a test can exercise
// the second refusal route: the user saying no to a prompt.
type askingBash struct{ *stubBash }

func (a askingBash) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Ask, Message: "allow this?"}
}

// TestRefusalFeedsNeitherBreaker is the invariant: a permission refusal is a
// decision about policy, not evidence that the call was wrong or the
// environment broken, so neither loop-breaker may count it.
//
// Both refusal routes are covered because both are revocable mid-Run — plan
// mode by an approved ExitPlanMode, an Approver denial by the user answering
// differently next time — and a counter fed by either outlives the decision
// that produced it.
func TestRefusalFeedsNeitherBreaker(t *testing.T) {
	tests := []struct {
		name     string
		tool     func(*stubBash) tools.Tool
		mode     permission.Mode
		approver Approver
		wantMsg  string
	}{
		{
			name:    "plan mode refuses command execution",
			tool:    func(b *stubBash) tools.Tool { return planModeBash{b} },
			mode:    permission.ModePlan,
			wantMsg: "plan mode is read-only",
		},
		{
			name:     "the user declines the approval prompt",
			tool:     func(b *stubBash) tools.Tool { return askingBash{b} },
			mode:     permission.ModeAutonomous,
			approver: DenyAll,
			wantMsg:  "allow this?",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bash := &stubBash{}
			l := New(nil, tools.NewRegistry(tc.tool(bash)))
			fs := newFailureState()
			opts := Options{
				Permission: permission.Context{Mode: permission.StaticMode(tc.mode)},
				Approver:   tc.approver,
			}

			// Enough refusals to trip either breaker twice over.
			for i := 0; i < repeatFailureLimit+2; i++ {
				tu := anthropic.BetaToolUseBlock{
					ID:    "t",
					Name:  "Bash",
					Input: json.RawMessage(bashInput("git status --short")),
				}
				got := resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs))
				if !strings.Contains(got, tc.wantMsg) {
					t.Fatalf("call %d: want the refusal %q, got %q", i+1, tc.wantMsg, got)
				}
			}
			if len(bash.ran) != 0 {
				t.Fatalf("a refused command ran: %v", bash.ran)
			}
			if len(fs.streaks) != 0 {
				t.Errorf("refusals fed loop-breaker B: %+v", fs.streaks)
			}
			if len(fs.failures) != 0 {
				t.Errorf("refusals fed loop-breaker A: %+v", fs.failures)
			}
		})
	}
}

// TestApprovedPlanCanRunTheCommandItWasRefused is the bug as the user met it.
//
// Plan mode refuses two commands. The plan is approved and the session becomes
// autonomous. Before the fix the next Bash call never reached the permission
// check: loop-breaker B had latched the tool on the strength of two identical
// refusal messages across different inputs, and answered with a quote of the
// plan-mode error the session had just escaped — wrapped in "Bash has failed
// with the SAME error 2 times in a row", which blamed the model for a refusal
// the harness had generated.
func TestApprovedPlanCanRunTheCommandItWasRefused(t *testing.T) {
	bash := &stubBash{}
	l := New(nil, tools.NewRegistry(planModeBash{bash}))
	fs := newFailureState()

	// A live mode, as every interactive frontend supplies: approving a plan
	// flips it in the middle of the Run.
	mode := permission.ModePlan
	opts := Options{Permission: permission.Context{Mode: func() permission.Mode { return mode }}}

	run := func(cmd string) string {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(bashInput(cmd))}
		return resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs))
	}

	for _, cmd := range []string{"git log --oneline -5", "go build ./..."} {
		if got := run(cmd); !strings.Contains(got, "plan mode is read-only") {
			t.Fatalf("%q: want the plan-mode refusal, got %q", cmd, got)
		}
	}

	mode = permission.ModeAutonomous // ExitPlanMode approved

	got := run("go build ./...")
	if len(bash.ran) != 1 || bash.ran[0] != "go build ./..." {
		t.Fatalf("the command refused in plan mode never ran after approval (ran=%v, result=%q)", bash.ran, got)
	}
	if strings.Contains(got, "SAME error") {
		t.Errorf("the harness blamed the model for its own refusal: %q", got)
	}
}
