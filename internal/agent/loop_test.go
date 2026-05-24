package agent

import (
	"context"
	"slices"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread/klaudia/internal/tools"
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
