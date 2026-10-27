package agent

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/plan"
)

// The registry is shared by every conversation in the process, and TodoWrite
// used to write to the store it was built with whatever conversation called
// it. A general-purpose sub-agent gets every tool, so its working notes
// replaced the user's approved plan; and the loop announced Options.Plan while
// the tool wrote somewhere else, so a frontend could show a list the tools had
// not touched. The conversation's store must be the one written, and nothing
// else.
func TestTodoWriteLandsInTheConversationsOwnPlan(t *testing.T) {
	childList := map[string]any{"todos": []map[string]any{
		{"content": "child step", "status": "in_progress", "activeForm": "Doing child step"},
	}}
	userSteps := []plan.Item{
		{Content: "user step 1", Status: plan.StatusInProgress, ActiveForm: "Doing user step 1"},
		{Content: "user step 2", Status: plan.StatusPending, ActiveForm: "Doing user step 2"},
	}

	tests := []struct {
		name string
		// run drives one TodoWrite call through either a sub-agent or the
		// parent loop, against a registry whose TodoWrite was built on built.
		run func(t *testing.T, reg *tools.Registry, provider api.Provider, conversation *plan.Store)
		// wantBuilt / wantConversation: the step contents each store must hold
		// afterwards.
		wantBuilt        []string
		wantConversation []string
	}{
		{
			name: "a sub-agent's TodoWrite leaves the user's plan alone",
			run: func(t *testing.T, reg *tools.Registry, provider api.Provider, _ *plan.Store) {
				s := newTestSpawner(provider, reg, "claude-opus-4-8",
					permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)}, nil, 2).
					WithWorkingDir(t.TempDir())
				if _, err := s.Spawn(context.Background(), "general-purpose", "work", nil); err != nil {
					t.Fatalf("Spawn: %v", err)
				}
			},
			wantBuilt: []string{"user step 1", "user step 2"},
		},
		{
			name: "the loop's own store is the one written",
			run: func(t *testing.T, reg *tools.Registry, provider api.Provider, conversation *plan.Store) {
				_, err := New(provider, reg).Run(context.Background(), Options{
					Prompt:     "work",
					Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
					Plan:       conversation,
				}, nil)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
			},
			wantBuilt:        []string{"user step 1", "user step 2"},
			wantConversation: []string{"child step"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			built := plan.New("")
			if err := built.Replace(userSteps); err != nil {
				t.Fatal(err)
			}
			conversation := plan.New("")
			todo, err := tools.NewTodoWrite(built)
			if err != nil {
				t.Fatal(err)
			}
			provider := &scriptedProvider{turns: []anthropic.BetaMessage{
				toolUseTurn(t, "tu1", "TodoWrite", childList),
			}}

			tc.run(t, tools.NewRegistry(todo), provider, conversation)

			if got := contents(built.Items()); !equalStrings(got, tc.wantBuilt) {
				t.Errorf("registry's store = %v, want %v", got, tc.wantBuilt)
			}
			if got := contents(conversation.Items()); !equalStrings(got, tc.wantConversation) {
				t.Errorf("conversation's store = %v, want %v", got, tc.wantConversation)
			}
		})
	}
}

func contents(items []plan.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Content)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
