package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// A dropped web search or fetch is the model's business, not the user's. The
// user-facing notice fired in sessions where nothing had gone wrong (a healthy
// deferred fetch was misread as corrupt), and a user seeing "an earlier web
// fetch was interrupted" reasonably went looking for a fault. So: no notice on
// screen, and the model is told once — the placeholder replaces the exchange
// in the kept history, and a later turn must neither re-announce it nor add a
// second note.
func TestADroppedSearchIsANoteToTheModelOnce(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("what's new?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock("Looking."),
			webSearchUse("srvtoolu_lost"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("carry on")),
	}
	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		{StopReason: "end_turn", Content: []anthropic.BetaContentBlockUnion{{Type: "text", Text: "ok"}}},
		{StopReason: "end_turn", Content: []anthropic.BetaContentBlockUnion{{Type: "text", Text: "ok"}}},
	}}
	loop := New(provider, tools.NewRegistry())

	var shown []string
	emit := func(e Event) {
		if e.Type == "notice" && strings.Contains(e.Content, "dropped") {
			shown = append(shown, e.Content)
		}
	}
	placeholders := func(msgs []anthropic.BetaMessageParam) int {
		n := 0
		for _, m := range msgs {
			for _, b := range m.Content {
				if b.OfText != nil && b.OfText.Text == droppedServerToolPlaceholder {
					n++
				}
				if su := b.OfServerToolUse; su != nil && su.ID == "srvtoolu_lost" {
					t.Fatal("the orphan survived in the returned conversation")
				}
			}
		}
		return n
	}

	res, err := loop.Run(context.Background(), Options{Prompt: "turn one", InitialMessages: history}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if n := placeholders(res.Messages); n != 1 {
		t.Fatalf("turn one left %d notes for the model, want 1", n)
	}
	// The next turn resumes from what the last one returned, as the TUI does.
	res, err = loop.Run(context.Background(), Options{Prompt: "turn two", InitialMessages: res.Messages}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if n := placeholders(res.Messages); n != 1 {
		t.Errorf("turn two left %d notes for the model, want still 1", n)
	}
	if len(shown) != 0 {
		t.Errorf("the user was shown %q; a dropped search is a note to the model only", shown)
	}
}
