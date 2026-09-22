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

// TestRuleMCPServerScope pins the MCP rule forms the JS reference documents:
// "mcp__<server>" and "mcp__<server>__*" cover every tool on that server, while
// "mcp__<server>__<tool>" covers just the one. Before this, a config that
// allowed "mcp__loki" matched nothing — the rule tool name was compared for
// equality against "mcp__loki__loki_query" — so a headless embedder that
// relied on its allow list was asked anyway (issue: stream-json control_request
// emitted for an allow-listed MCP tool).
func TestRuleMCPServerScope(t *testing.T) {
	tests := []struct {
		rule string
		tool string
		want bool
	}{
		{"mcp__loki", "mcp__loki__loki_query", true},
		{"mcp__loki__*", "mcp__loki__loki_query", true},
		{"mcp__loki__loki_query", "mcp__loki__loki_query", true},
		{"mcp__loki", "mcp__loki", true}, // exact name still matches itself
		// Server scope is the whole server name, not a prefix of it.
		{"mcp__loki", "mcp__lokitwo__query", false},
		{"mcp__lok", "mcp__loki__query", false},
		// One tool's rule does not spread to its siblings.
		{"mcp__loki__loki_query", "mcp__loki__label_values", false},
		// Not MCP at all.
		{"Read", "mcp__loki__loki_query", false},
		{"mcp__", "mcp__loki__loki_query", false},
	}
	for _, tt := range tests {
		r := Rule{Tool: tt.rule}
		// MCP tools report their own qualified name as the specifier.
		if got := r.matches(tt.tool, tt.tool); got != tt.want {
			t.Errorf("Rule{%q}.matches(%q) = %v, want %v", tt.rule, tt.tool, got, tt.want)
		}
	}
}

// TestCheckMCPServerRules runs the server-scoped forms through the full Check
// flow, in both directions: an allow rule short-circuits the tool's own "ask",
// and a deny rule wins even under bypassPermissions.
func TestCheckMCPServerRules(t *testing.T) {
	tool := fakeTool{name: "mcp__loki__loki_query", intrinsic: Decision{Behavior: Ask}}
	req := PermissionRequest{Specifier: tool.name}

	allow := Context{Mode: StaticMode(ModeDefault), Allow: []Rule{{Tool: "mcp__loki"}}}
	if got := Check(allow, tool, req); got.Behavior != Allow {
		t.Errorf("allow mcp__loki: behavior = %q, want allow", got.Behavior)
	}
	other := Context{Mode: StaticMode(ModeDefault), Allow: []Rule{{Tool: "mcp__prom"}}}
	if got := Check(other, tool, req); got.Behavior != Ask {
		t.Errorf("allow mcp__prom only: behavior = %q, want ask", got.Behavior)
	}
	deny := Context{Mode: StaticMode(ModeBypassPermissions), Deny: []Rule{{Tool: "mcp__loki__*"}}}
	if got := Check(deny, tool, req); got.Behavior != Deny {
		t.Errorf("deny mcp__loki__*: behavior = %q, want deny", got.Behavior)
	}
}
