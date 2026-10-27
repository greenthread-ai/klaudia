package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

type thinkingRejectProvider struct {
	calls int
	seen  []int // messages per request
}

func (p *thinkingRejectProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.calls++
	p.seen = append(p.seen, len(params.Messages))
	if p.calls == 1 {
		return anthropic.BetaMessage{}, errors.New("messages.1.content.42: `thinking` or `redacted_thinking` blocks in the latest assistant message cannot be modified. These blocks must remain as they were in the original response.")
	}
	return anthropic.BetaMessage{
		StopReason: "end_turn",
		Content:    []anthropic.BetaContentBlockUnion{{Type: "text", Text: "carried on"}},
	}, nil
}

// Reported from a live session: every send returned this 400, so the
// conversation could not be continued at all. The turn is abandoned and retaken
// rather than resent.
func TestImmutableThinkingIsRecovered(t *testing.T) {
	provider := &thinkingRejectProvider{}
	loop := New(provider, tools.NewRegistry())

	var notices []string
	_, err := loop.Run(context.Background(), Options{
		Prompt: "continue",
		InitialMessages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("find the schema")),
			// Deliberately a turn sanitising leaves alone, so this exercises
			// recovery rather than the preventive drop: the API can reject a
			// repaired message for reasons we cannot predict from here
			// (micro-compaction, an older transcript, a future repair).
			{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
				anthropic.NewBetaThinkingBlock("sig", "thinking"),
				anthropic.NewBetaTextBlock("Looking into it."),
			}},
		},
	}, func(e Event) {
		if e.Type == "notice" {
			notices = append(notices, e.Content)
		}
	})
	if err != nil {
		t.Fatalf("the session must continue, got %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider called %d times, want 2 (reject then retry)", provider.calls)
	}
	if provider.seen[1] >= provider.seen[0] {
		t.Errorf("retry sent %d messages vs %d — the bad turn was not dropped", provider.seen[1], provider.seen[0])
	}
	if len(notices) == 0 {
		t.Error("the user should be told the turn was retaken")
	}
}

// The classifier must not fire on unrelated 400s.
func TestImmutableThinkingClassifier(t *testing.T) {
	if !api.IsImmutableThinking(errors.New("`thinking` or `redacted_thinking` blocks in the latest assistant message cannot be modified")) {
		t.Error("should match the reported wording")
	}
	if api.IsImmutableThinking(errors.New("messages.0.content: Field required")) {
		t.Error("should not match an unrelated bad request")
	}
	if api.IsImmutableThinking(nil) {
		t.Error("nil is not an error")
	}
}
