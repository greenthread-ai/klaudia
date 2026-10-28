package agent

import (
	"context"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// AskUserQuestion is offered only when someone can answer: a headless run or
// a sub-agent has no asker, and offering the tool there only invites calls
// that fail. (PR #13.)
func TestAskOnlyToolsNeedAnAsker(t *testing.T) {
	ask, err := tools.NewAskUserQuestion()
	if err != nil {
		t.Fatal(err)
	}
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(ask, read))
	has := func(canAsk bool) bool {
		ps, err := l.buildToolParams(context.Background(), nil, nil, canAsk)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.OfTool.Name == "AskUserQuestion" {
				return true
			}
		}
		return false
	}
	if has(false) {
		t.Error("AskUserQuestion offered with no one to ask")
	}
	if !has(true) {
		t.Error("AskUserQuestion missing with an asker")
	}
}
