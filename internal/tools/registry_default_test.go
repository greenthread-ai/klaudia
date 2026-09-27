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

// A run with no interactive frontend (headless -p, --loop, stream-json embedding)
// must not offer AskUserQuestion: shown the tool, a model reaches for it on any
// ambiguity and burns a turn learning nobody is there. Everything else stays.
func TestWithoutFrontendDropsAskUserQuestion(t *testing.T) {
	reg, err := DefaultRegistry(nil, WithoutFrontend())
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	if _, ok := reg.Lookup("AskUserQuestion"); ok {
		t.Errorf("AskUserQuestion registered without a frontend")
	}
	full, err := DefaultRegistry(nil)
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	if got, want := len(reg.Names()), len(full.Names())-1; got != want {
		t.Errorf("registry has %d tools, want %d (all but AskUserQuestion)", got, want)
	}
	for _, name := range []string{"Read", "Bash", "ExitPlanMode", "RequestHostChange"} {
		if _, ok := reg.Lookup(name); !ok {
			t.Errorf("tool %q missing without a frontend", name)
		}
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

func mustWrite(t *testing.T) *Write {
	t.Helper()
	w, err := NewWrite()
	if err != nil {
		t.Fatal(err)
	}
	return w
}
