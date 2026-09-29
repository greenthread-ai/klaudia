package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// bigToolHistory is a conversation whose tool results are large enough for
// microcompact to act on: five tool rounds of ~15k tokens each, every
// assistant turn carrying a signed thinking block the way the API returns it.
func bigToolHistory() []anthropic.BetaMessageParam {
	big := strings.Repeat("x", 60000)
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("start")),
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("t%d", i)
		msgs = append(msgs,
			anthropic.BetaMessageParam{
				Role: anthropic.BetaMessageParamRoleAssistant,
				Content: []anthropic.BetaContentBlockParamUnion{
					anthropic.NewBetaThinkingBlock(fmt.Sprintf("sig-%d", i), ""),
					anthropic.NewBetaToolUseBlock(id, map[string]any{"note": id}, "Recorder"),
				},
			},
			anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock(id, big, false)),
		)
	}
	return msgs
}

// firstToolResultText returns the text of the first tool_result in a request.
func firstToolResultText(t *testing.T, msgs []anthropic.BetaMessageParam) string {
	t.Helper()
	for _, m := range msgs {
		for _, b := range m.Content {
			if tr := b.OfToolResult; tr != nil {
				var sb strings.Builder
				for _, c := range tr.Content {
					if c.OfText != nil {
						sb.WriteString(c.OfText.Text)
					}
				}
				return sb.String()
			}
		}
	}
	t.Fatal("request carries no tool_result")
	return ""
}

// On a model with preserved thinking, every thinking block after an old tool
// result is bound to that result's bytes. Microcompact eliding it would fail
// the check on every later request, so it must leave already-sent tool results
// alone on those models — and keep working on the others.
func TestMicrocompactLeavesSentToolResultsAloneOnPreservedThinkingModels(t *testing.T) {
	for _, tc := range []struct {
		model      anthropic.Model
		wantElided bool
	}{
		{"claude-fable-5-1", false},
		{"claude-opus-5-5", false},
		{"claude-opus-5", true},
	} {
		t.Run(string(tc.model), func(t *testing.T) {
			provider := &recordingProvider{}
			loop := New(provider, tools.NewRegistry())
			_, err := loop.Run(context.Background(), Options{
				Model:           tc.model,
				Prompt:          "carry on",
				InitialMessages: bigToolHistory(),
				ContextWindow:   1_000_000, // far from autocompact
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(provider.requests) != 1 {
				t.Fatalf("got %d requests, want 1", len(provider.requests))
			}
			got := firstToolResultText(t, provider.requests[0])
			elided := got != strings.Repeat("x", 60000)
			if elided != tc.wantElided {
				t.Errorf("oldest tool result elided = %v, want %v (content %.40q…)", elided, tc.wantElided, got)
			}
		})
	}
}
