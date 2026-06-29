package subagent

import (
	"sort"
	"testing"

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
