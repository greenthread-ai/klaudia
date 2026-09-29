package tools

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"testing"
)

func TestDefaultRegistryLoadsAllTools(t *testing.T) {
	reg, err := DefaultRegistry(nil)
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	want := []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash", "BashOutput", "KillShell", "Jobs", "RestartJob", "TodoWrite", "TaskCreate", "TaskList", "TaskGet", "TaskUpdate", "NotebookEdit", "AskUserQuestion", "ExitPlanMode", "RequestHostChange", "BrowserSearch", "BrowserFetch", "BrowserNavigate", "BrowserSnapshot"}
	for _, name := range want {
		tool, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		// Every tool must advertise a non-empty description and a valid
		// object input schema.
		desc, err := tool.Description(context.Background())
		if err != nil || desc == "" {
			t.Errorf("%s: description err=%v empty=%v", name, err, desc == "")
		}
		var m map[string]any
		if err := json.Unmarshal(tool.InputSchema(), &m); err != nil {
			t.Errorf("%s: input schema invalid: %v", name, err)
		}
		if m["type"] != "object" {
			t.Errorf("%s: schema type = %v, want object", name, m["type"])
		}
	}
	if got := len(reg.Names()); got != len(want) {
		t.Errorf("registry has %d tools, want %d", got, len(want))
	}
}

// Names must be stable (sorted) across calls. The tool order drives the request
// sent every turn; a varying order changes the cached prefix each turn and
// defeats prompt caching. Regression guard for the map-iteration order bug.
func TestRegistryNamesAreStableAndSorted(t *testing.T) {
	reg := NewRegistry(mustRead(t), mustWrite(t))
	first := reg.Names()
	if !sort.StringsAreSorted(first) {
		t.Errorf("Names() not sorted: %v", first)
	}
	// Repeated calls return the identical order (not just any deterministic one).
	for i := 0; i < 20; i++ {
		if !slices.Equal(reg.Names(), first) {
			t.Fatalf("Names() order varies between calls: %v vs %v", reg.Names(), first)
		}
	}
}

// The deferred set must name the Browser* and Task* tools (revealed on demand
// via ToolSearch), every deferred tool must still be registered so it works
// once revealed, and the common core (edit/read/bash/search + TodoWrite) must
// stay eager — the whole point is to trim the standing list without losing a
// capability.
func TestDefaultDeferredTools(t *testing.T) {
	deferred := DefaultDeferredTools()
	want := []string{
		"BrowserSearch", "BrowserFetch", "BrowserNavigate", "BrowserSnapshot",
		"TaskCreate", "TaskList", "TaskGet", "TaskUpdate",
	}
	if !slices.Equal(append([]string(nil), deferred...), want) {
		t.Errorf("DefaultDeferredTools() = %v, want %v", deferred, want)
	}

	reg, err := DefaultRegistry(nil)
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	// Every deferred tool is still registered — deferral withholds it from the
	// request, it does not remove it — so ToolSearch can reveal a working tool.
	for _, name := range deferred {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("deferred tool %q is not registered; ToolSearch could not reveal it", name)
		}
	}

	// The commonly-used core stays eager: it must never appear in the deferred
	// set. Paying a ToolSearch round-trip to reach Read or Bash would be a loss.
	deferredSet := make(map[string]bool, len(deferred))
	for _, name := range deferred {
		deferredSet[name] = true
	}
	for _, core := range []string{"Read", "Write", "Edit", "Glob", "Grep", "Bash", "BashOutput", "TodoWrite"} {
		if deferredSet[core] {
			t.Errorf("core tool %q must stay eager, but it is in the deferred set", core)
		}
	}
}

func mustWrite(t *testing.T) *Write {
	t.Helper()
	w, err := NewWrite()
	if err != nil {
		t.Fatal(err)
	}
	return w
}
