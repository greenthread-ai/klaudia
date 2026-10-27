package permission

import "testing"

// fakeTool implements IntrinsicChecker for testing the central Check flow.
type fakeTool struct {
	name      string
	intrinsic Decision
}

func (f fakeTool) Name() string { return f.name }
func (f fakeTool) CheckPermissions(Context, PermissionRequest) Decision {
	return f.intrinsic
}

func TestCheckBypassAllows(t *testing.T) {
	pctx := Context{Mode: StaticMode(ModeBypassPermissions)}
	tool := fakeTool{name: "Bash", intrinsic: Decision{Behavior: Deny}}
	if got := Check(pctx, tool, PermissionRequest{Specifier: "ls"}); got.Behavior != Allow {
		t.Errorf("bypass behavior = %q, want allow", got.Behavior)
	}
}

func TestCheckFallsThroughToIntrinsic(t *testing.T) {
	pctx := Context{Mode: StaticMode(ModePlan)}
	tool := fakeTool{name: "Write", intrinsic: Decision{Behavior: Deny, Message: "read-only"}}
	got := Check(pctx, tool, PermissionRequest{})
	if got.Behavior != Deny || got.Message != "read-only" {
		t.Errorf("decision = %+v, want the tool's own deny", got)
	}
}

// A zero-value Context is what a test or an embedding caller builds when it
// has no session to read a mode from. It must not land on a mode that blocks
// ordinary work: the host gate runs ahead of this package and is unaffected by
// the mode, so there is nothing for a restrictive default to protect.
func TestZeroValueContextIsAutonomous(t *testing.T) {
	if got := CurrentMode(Context{}); got != ModeAutonomous {
		t.Errorf("CurrentMode(zero) = %q, want autonomous", got)
	}
}

// TestCheckModeIsLive pins the bug this shape exists to prevent: a Context
// built once at turn start used to freeze the mode for every inner check, so
// a `/mode bypass` mid-turn didn't take effect until the next TUI turn —
// painful in a long /goal iteration. Mode is a function that re-reads the
// live session setting on every Check.
func TestCheckModeIsLive(t *testing.T) {
	current := ModePlan
	pctx := Context{Mode: func() Mode { return current }}
	tool := fakeTool{name: "Write", intrinsic: Decision{Behavior: Deny}}

	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Deny {
		t.Fatalf("plan mode: behavior = %q, want deny", got.Behavior)
	}
	// Flip the live source between calls — same Context, new mode picked up.
	current = ModeBypassPermissions
	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Allow {
		t.Errorf("after live flip to bypass: behavior = %q, want allow", got.Behavior)
	}
	current = ModePlan
	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Deny {
		t.Errorf("after live flip back to plan: behavior = %q, want deny", got.Behavior)
	}
}

// The legacy modes are gone, not merely unlisted. A config carrying one must
// fail loudly at startup rather than resolve to something that looks like it
// worked.
func TestLegacyModesAreNoLongerValid(t *testing.T) {
	for _, m := range []Mode{"default", "acceptEdits", "dontAsk"} {
		if m.Valid() {
			t.Errorf("mode %q is still valid", m)
		}
	}
	for _, m := range SelectableModes() {
		if !m.Valid() {
			t.Errorf("selectable mode %q is not valid", m)
		}
	}
}
