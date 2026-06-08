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

func TestCheckDenyRuleWins(t *testing.T) {
	pctx := Context{
		Mode: StaticMode(ModeBypassPermissions), // even bypass must lose to an explicit deny
		Deny: []Rule{{Tool: "Bash", Specifier: "rm -rf:*"}},
	}
	tool := fakeTool{name: "Bash", intrinsic: Decision{Behavior: Allow}}
	got := Check(pctx, tool, PermissionRequest{Specifier: "rm -rf /tmp/x"})
	if got.Behavior != Deny {
		t.Errorf("behavior = %q, want deny", got.Behavior)
	}
}

func TestCheckBypassAllows(t *testing.T) {
	pctx := Context{Mode: StaticMode(ModeBypassPermissions)}
	tool := fakeTool{name: "Bash", intrinsic: Decision{Behavior: Ask}}
	if got := Check(pctx, tool, PermissionRequest{Specifier: "ls"}); got.Behavior != Allow {
		t.Errorf("bypass behavior = %q, want allow", got.Behavior)
	}
}

func TestCheckAllowRule(t *testing.T) {
	pctx := Context{
		Mode:  StaticMode(ModeDefault),
		Allow: []Rule{{Tool: "Bash", Specifier: "git status"}},
	}
	tool := fakeTool{name: "Bash", intrinsic: Decision{Behavior: Ask}}
	if got := Check(pctx, tool, PermissionRequest{Specifier: "git status"}); got.Behavior != Allow {
		t.Errorf("allow-rule behavior = %q, want allow", got.Behavior)
	}
	// A non-matching specifier falls through to the intrinsic (ask).
	if got := Check(pctx, tool, PermissionRequest{Specifier: "git push"}); got.Behavior != Ask {
		t.Errorf("non-matching behavior = %q, want ask", got.Behavior)
	}
}

func TestCheckFallsThroughToIntrinsic(t *testing.T) {
	pctx := Context{Mode: StaticMode(ModeDefault)}
	tool := fakeTool{name: "Write", intrinsic: Decision{Behavior: Ask}}
	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Ask {
		t.Errorf("behavior = %q, want ask", got.Behavior)
	}
}

// TestCheckModeIsLive pins the actual bug we fixed: a Context built once at
// turn start used to freeze the mode for every inner permission check, so a
// `/mode bypass` mid-turn didn't take effect until the next TUI turn —
// painful in a long /goal iteration. Now Mode is a function that re-reads
// the live session setting on every Check.
func TestCheckModeIsLive(t *testing.T) {
	current := ModeDefault
	pctx := Context{Mode: func() Mode { return current }}
	tool := fakeTool{name: "Write", intrinsic: Decision{Behavior: Ask}}

	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Ask {
		t.Fatalf("default mode: behavior = %q, want ask", got.Behavior)
	}
	// Flip the live source between calls — same Context, new mode picked up.
	current = ModeBypassPermissions
	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Allow {
		t.Errorf("after live flip to bypass: behavior = %q, want allow", got.Behavior)
	}
	current = ModeDefault
	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Ask {
		t.Errorf("after live flip back to default: behavior = %q, want ask", got.Behavior)
	}
}

func TestRulePrefixMatch(t *testing.T) {
	r := Rule{Tool: "Bash", Specifier: "npm run:*"}
	if !r.matches("Bash", "npm run build") {
		t.Error("expected prefix match for 'npm run:*'")
	}
	if r.matches("Bash", "npm install") {
		t.Error("did not expect match for 'npm install'")
	}
}
