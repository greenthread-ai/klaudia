package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/plan"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

type stubAsker struct{}

func (stubAsker) Ask(context.Context, string, []tools.AskOption) (string, error) { return "", nil }

type stubPlanner struct{}

func (stubPlanner) ExitPlan(context.Context, string) (bool, error) { return false, nil }

type stubRecorder struct{}

func (stubRecorder) Record(string, json.RawMessage) error { return nil }

// fullTurn is a Turn with every field set to something observably non-zero.
func fullTurn() Turn {
	return Turn{
		Prompt:     "do the thing",
		History:    []anthropic.BetaMessageParam{{Role: anthropic.BetaMessageParamRoleUser}},
		Emit:       func(Event) {},
		Approver:   DenyAll,
		Asker:      stubAsker{},
		Planner:    stubPlanner{},
		Interject:  func() Interjection { return Interjection{} },
		BeforeEdit: func(string, []string) {},
		Mode:       permission.StaticMode(permission.ModeBypassPermissions),
		Recorder:   stubRecorder{},
		Plan:       plan.New(""),
		ReadText: func(context.Context, string, int, int) (string, error) {
			return "", nil
		},
		Background: NewBackground(context.Background(), nil),
	}
}

func TestTurnApplyCopiesEveryField(t *testing.T) {
	// optionsField names the Options field each Turn field must reach. Emit is
	// the one exception and is named here rather than left out, so that "not
	// copied" is a decision the test records rather than an omission it shares.
	optionsField := map[string]string{
		"Prompt":     "Prompt",
		"History":    "InitialMessages",
		"Emit":       "", // passed to Loop.Run alongside Options, not through it
		"Approver":   "Approver",
		"Asker":      "Asker",
		"Planner":    "Planner",
		"Interject":  "Interject",
		"BeforeEdit": "BeforeEdit",
		"Mode":       "Permission",
		"Recorder":   "Recorder",
		"Plan":       "Plan",
		"ReadText":   "ReadText",
		"Background": "Background",
	}

	var opts Options
	fullTurn().Apply(&opts)

	tt := reflect.TypeOf(Turn{})
	ov := reflect.ValueOf(opts)
	for i := 0; i < tt.NumField(); i++ {
		name := tt.Field(i).Name
		target, known := optionsField[name]
		if !known {
			// A new Turn field that Apply does not copy is precisely the bug
			// this guards: stream-json ran for months with Asker and Planner
			// silently nil because the copying was spelled out per frontend.
			t.Errorf("Turn.%s is not in the mapping — does Apply copy it?", name)
			continue
		}
		if target == "" {
			continue
		}
		f := ov.FieldByName(target)
		if !f.IsValid() {
			t.Errorf("Turn.%s maps to Options.%s, which does not exist", name, target)
			continue
		}
		if f.IsZero() {
			t.Errorf("Turn.%s did not reach Options.%s", name, target)
		}
	}
}

func TestTurnApplyLeavesOtherOptionsAlone(t *testing.T) {
	// Apply is called on an Options the CLI has already filled in with the
	// per-run settings. Overwriting any of them would silently undo the model,
	// system prompt or permission mode the mode had resolved.
	opts := Options{
		WorkingDir:    "/project",
		Model:         "claude-sonnet-4-5",
		System:        "system prompt",
		MaxTurns:      7,
		MaxTokens:     4096,
		ContextWindow: 200000,
		Permission:    permission.Context{Mode: permission.StaticMode(permission.ModePlan)},
		WebTools:      true,
		DeferredTools: map[string]bool{"mcp__x__y": true},
		SubAgent:      true,
	}
	before := opts
	// Mode cleared deliberately: a turn that supplies one is *meant* to replace
	// the permission context, which TestTurnModeOverridesThePermissionContext
	// covers. This test is about the fields no turn may touch.
	turn := fullTurn()
	turn.Mode = nil
	turn.Apply(&opts)

	if opts.WorkingDir != before.WorkingDir || opts.Model != before.Model ||
		opts.System != before.System || opts.MaxTurns != before.MaxTurns ||
		opts.MaxTokens != before.MaxTokens || opts.ContextWindow != before.ContextWindow ||
		opts.WebTools != before.WebTools || opts.SubAgent != before.SubAgent ||
		len(opts.DeferredTools) != len(before.DeferredTools) {
		t.Errorf("Apply changed a per-run field:\nbefore %+v\nafter  %+v", before, opts)
	}
	if opts.Permission.Mode == nil || opts.Permission.Mode() != permission.ModePlan {
		t.Error("Apply clobbered the permission context")
	}
}

func TestTurnApplyZeroTurnClearsNothingUnexpected(t *testing.T) {
	// A frontend that supplies no Asker gets a nil one, which is the documented
	// way to say "there is nobody to ask". It must not inherit a stale one from
	// whatever the Options already held.
	opts := Options{Asker: stubAsker{}, Planner: stubPlanner{}}
	Turn{Prompt: "hi"}.Apply(&opts)
	if opts.Asker != nil || opts.Planner != nil {
		t.Error("a Turn with no Asker/Planner must clear them, not leave the previous turn's behind")
	}
	if opts.Prompt != "hi" {
		t.Errorf("Prompt = %q, want %q", opts.Prompt, "hi")
	}
}

func TestTurnModeOverridesThePermissionContext(t *testing.T) {
	// The ACP frontend keeps a permission mode per editor session, so the mode
	// the CLI resolved at startup is not the one that should apply. A turn that
	// names a mode wins; a turn that does not leaves the CLI's alone.
	tests := []struct {
		name string
		turn func() permission.Mode
		want permission.Mode
	}{
		{
			name: "turn supplies a live mode",
			turn: permission.StaticMode(permission.ModePlan),
			want: permission.ModePlan,
		},
		{
			name: "turn supplies none",
			turn: nil,
			want: permission.ModeBypassPermissions,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := Options{Permission: permission.Context{
				Mode: permission.StaticMode(permission.ModeBypassPermissions),
			}}
			Turn{Mode: tc.turn}.Apply(&opts)
			if got := permission.CurrentMode(opts.Permission); got != tc.want {
				t.Errorf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTurnModeIsReadAtCheckTimeNotApplyTime(t *testing.T) {
	// A function, not a value: ExitPlanMode being approved mid-turn has to
	// reach the next tool dispatch. Copying the mode's value at Apply would
	// leave the rest of the turn blocked by the plan mode the user just left.
	mode := permission.ModePlan
	opts := Options{}
	Turn{Mode: func() permission.Mode { return mode }}.Apply(&opts)

	if got := permission.CurrentMode(opts.Permission); got != permission.ModePlan {
		t.Fatalf("mode = %q, want %q", got, permission.ModePlan)
	}
	mode = permission.ModeAutonomous
	if got := permission.CurrentMode(opts.Permission); got != permission.ModeAutonomous {
		t.Errorf("after the mode changed, mode = %q, want %q", got, permission.ModeAutonomous)
	}
}
