package agent

import (
	"context"
	"slices"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

func namesOf(params []anthropic.BetaToolUnionParam) []string {
	var out []string
	for _, p := range params {
		if p.OfTool != nil {
			out = append(out, p.OfTool.Name)
		}
	}
	return out
}

func TestBuildToolParamsDeferral(t *testing.T) {
	read, _ := tools.NewRead()
	write, _ := tools.NewWrite()
	reg := tools.NewRegistry(read, write)
	l := New(nil, reg) // provider is unused by buildToolParams

	deferred := map[string]bool{"Write": true}

	// Write is deferred and not yet revealed → only Read is offered.
	params, err := l.buildToolParams(context.Background(), deferred, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	names := namesOf(params)
	if !slices.Contains(names, "Read") || slices.Contains(names, "Write") {
		t.Errorf("before reveal: got %v, want [Read] only", names)
	}

	// Once revealed, Write is offered too.
	params, _ = l.buildToolParams(context.Background(), deferred, map[string]bool{"Write": true})
	names = namesOf(params)
	if !slices.Contains(names, "Read") || !slices.Contains(names, "Write") {
		t.Errorf("after reveal: got %v, want Read+Write", names)
	}
}

// With the real default registry and default deferred set, the Browser* and
// Task* tools are withheld from the initial request while the common core and
// ToolSearch stay eager; a Browser tool becomes available only after ToolSearch
// reveals it. This is the trimmed standing toolset the issue asked for.
func TestBuildToolParamsDefersBrowserAndTaskTools(t *testing.T) {
	reg, err := tools.DefaultRegistry(nil)
	if err != nil {
		t.Fatalf("DefaultRegistry: %v", err)
	}
	catalog := make([]tools.ToolInfo, 0)
	deferred := map[string]bool{}
	for _, name := range reg.Names() {
		catalog = append(catalog, tools.ToolInfo{Name: name})
	}
	for _, name := range tools.DefaultDeferredTools() {
		deferred[name] = true
	}
	ts, err := tools.NewToolSearch(catalog)
	if err != nil {
		t.Fatalf("NewToolSearch: %v", err)
	}
	reg.Replace(append(reg.All(), ts)...)
	l := New(nil, reg)

	// Nothing revealed yet: Browser*/Task* are withheld, core + ToolSearch stay.
	params, err := l.buildToolParams(context.Background(), deferred, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	names := namesOf(params)
	for _, hidden := range tools.DefaultDeferredTools() {
		if slices.Contains(names, hidden) {
			t.Errorf("deferred tool %q offered before reveal: %v", hidden, names)
		}
	}
	for _, eager := range []string{"Read", "Edit", "Bash", "Grep", "TodoWrite", "ToolSearch"} {
		if !slices.Contains(names, eager) {
			t.Errorf("eager tool %q missing from initial request: %v", eager, names)
		}
	}

	// After ToolSearch reveals BrowserSearch, it is offered — still a working tool.
	params, _ = l.buildToolParams(context.Background(), deferred, map[string]bool{"BrowserSearch": true})
	if names = namesOf(params); !slices.Contains(names, "BrowserSearch") {
		t.Errorf("after reveal: BrowserSearch missing: %v", names)
	}
}
