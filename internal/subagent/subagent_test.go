package subagent

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestBuiltinLookup(t *testing.T) {
	for _, name := range []string{"general-purpose", "Explore", "Plan"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("built-in type %q not found", name)
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("unexpected type found")
	}
}

func TestFilterAllVsRestricted(t *testing.T) {
	base, err := tools.DefaultRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}

	gp, _ := Lookup("general-purpose")
	if got := len(gp.Filter(base).Names()); got != len(base.Names()) {
		t.Errorf("general-purpose filtered to %d tools, want all %d", got, len(base.Names()))
	}

	explore, _ := Lookup("Explore")
	names := explore.Filter(base).Names()
	sort.Strings(names)
	want := []string{"Glob", "Grep", "Read"}
	if len(names) != len(want) {
		t.Fatalf("Explore tools = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("Explore tools = %v, want %v", names, want)
			break
		}
	}
}

// stubTool is the minimum that satisfies tools.Tool; roStub adds the read-only
// declaration an MCP tool carries.
type stubTool struct{ name string }

func (s stubTool) Name() string                                { return s.name }
func (s stubTool) Description(context.Context) (string, error) { return "", nil }
func (s stubTool) InputSchema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) ValidateInput(json.RawMessage) error         { return nil }
func (s stubTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{Specifier: s.name}
}
func (s stubTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Ask}
}
func (s stubTool) Execute(context.Context, tools.Context, json.RawMessage) ([]tools.Result, error) {
	return nil, nil
}

type roStub struct {
	stubTool
	ro bool
}

func (r roStub) ReadOnly() bool { return r.ro }

// Fanning out across a wiki, an issue tracker and a chat archive is exactly
// what the read-only agents are for, and exactly the work whose bulk should
// never reach the main thread. They could not do it: their whitelist is three
// tool names, and MCP tool names are not known until connect time.
//
// The important half is the second assertion. Reaching MCP must not become a
// way to reach MCP's *writes* — mcp__gitea__delete_branch is one connected
// server away from an agent whose only guarantee was a system prompt asking it
// not to.
func TestReadOnlyAgentsGetReadOnlyMCPToolsAndNothingElse(t *testing.T) {
	base := tools.NewRegistry(
		stubTool{"Read"}, stubTool{"Glob"}, stubTool{"Grep"},
		stubTool{"Edit"}, stubTool{"Bash"},
		roStub{stubTool{"mcp__gitea__get_file_contents"}, true},
		roStub{stubTool{"mcp__gitea__search_issues"}, true},
		roStub{stubTool{"mcp__gitea__delete_branch"}, false},
		roStub{stubTool{"mcp__gitea__create_or_update_file"}, false},
	)

	for _, name := range []string{"Explore", "Plan"} {
		typ, ok := Lookup(name)
		if !ok {
			t.Fatalf("no %s type", name)
		}
		got := map[string]bool{}
		for _, n := range typ.Filter(base).Names() {
			got[n] = true
		}
		for _, want := range []string{"Read", "Glob", "Grep",
			"mcp__gitea__get_file_contents", "mcp__gitea__search_issues"} {
			if !got[want] {
				t.Errorf("%s: missing %s", name, want)
			}
		}
		for _, banned := range []string{"Edit", "Bash",
			"mcp__gitea__delete_branch", "mcp__gitea__create_or_update_file"} {
			if got[banned] {
				t.Errorf("%s: has %s, which writes", name, banned)
			}
		}
	}
}

// general-purpose already took everything and must keep doing so; the new field
// must not quietly become the only route to MCP.
func TestGeneralPurposeStillTakesEverything(t *testing.T) {
	base := tools.NewRegistry(
		stubTool{"Bash"},
		roStub{stubTool{"mcp__gitea__delete_branch"}, false},
	)
	typ, _ := Lookup("general-purpose")
	if n := len(typ.Filter(base).Names()); n != 2 {
		t.Errorf("general-purpose got %d tools, want 2", n)
	}
}
