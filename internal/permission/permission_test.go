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
		Mode: ModeBypassPermissions, // even bypass must lose to an explicit deny
		Deny: []Rule{{Tool: "Bash", Specifier: "rm -rf:*"}},
	}
	tool := fakeTool{name: "Bash", intrinsic: Decision{Behavior: Allow}}
	got := Check(pctx, tool, PermissionRequest{Specifier: "rm -rf /tmp/x"})
	if got.Behavior != Deny {
		t.Errorf("behavior = %q, want deny", got.Behavior)
	}
}

func TestCheckBypassAllows(t *testing.T) {
	pctx := Context{Mode: ModeBypassPermissions}
	tool := fakeTool{name: "Bash", intrinsic: Decision{Behavior: Ask}}
	if got := Check(pctx, tool, PermissionRequest{Specifier: "ls"}); got.Behavior != Allow {
		t.Errorf("bypass behavior = %q, want allow", got.Behavior)
	}
}

func TestCheckAllowRule(t *testing.T) {
	pctx := Context{
		Mode:  ModeDefault,
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
	pctx := Context{Mode: ModeDefault}
	tool := fakeTool{name: "Write", intrinsic: Decision{Behavior: Ask}}
	if got := Check(pctx, tool, PermissionRequest{}); got.Behavior != Ask {
		t.Errorf("behavior = %q, want ask", got.Behavior)
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
