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

	// Both are sent; only Write is marked defer_loading. A reveal does not
	// change this list (api/toolreveal.go).
	params, err := l.buildToolParams(context.Background(), deferred, true)
	if err != nil {
		t.Fatal(err)
	}
	if names := namesOf(params); !slices.Contains(names, "Read") || !slices.Contains(names, "Write") {
		t.Errorf("got %v, want Read and Write", names)
	}
	for _, p := range params {
		if got, want := p.OfTool.DeferLoading.Value, p.OfTool.Name == "Write"; got != want {
			t.Errorf("%s defer_loading = %v, want %v", p.OfTool.Name, got, want)
		}
	}
}
