package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// paramText concatenates the text of a user message param, for asserting what
// the loop injected into the conversation.
func paramText(m anthropic.BetaMessageParam) string {
	var b strings.Builder
	for _, block := range m.Content {
		if block.OfText != nil {
			b.WriteString(block.OfText.Text)
		}
	}
	return b.String()
}

// A finished background agent's report is injected as a user message on the next
// turn, so the model sees the result rather than never hearing about it.
func TestLoopDeliversBackgroundReport(t *testing.T) {
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		{StopReason: "end_turn"},
	}}
	l := New(provider, tools.NewRegistry())

	delivered := false
	res, err := l.Run(context.Background(), Options{
		Prompt: "carry on",
		Model:  "claude-opus-4-8",
		CollectBackground: func() string {
			if delivered {
				return ""
			}
			delivered = true
			return "Background sub-agent(s) finished.\n\n[agent-1 · Explore · succeeded]\nfound the bug in loop.go"
		},
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	var found bool
	for _, m := range res.Messages {
		if strings.Contains(paramText(m), "found the bug in loop.go") {
			found = true
		}
	}
	if !found {
		t.Fatal("the background report never reached the conversation")
	}
}

// A nil CollectBackground (headless single-shot) must not panic and must inject
// nothing.
func TestLoopWithoutBackgroundCollectorIsSafe(t *testing.T) {
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{{StopReason: "end_turn"}}}
	l := New(provider, tools.NewRegistry())
	if _, err := l.Run(context.Background(), Options{Prompt: "hi", Model: "claude-opus-4-8"}, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
}
