package compaction

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// The summary request is sent without the conversation's system prompt and
// tools. On a model with preserved thinking a thinking block replayed there no
// longer matches the conversation it was minted in and the request is refused,
// so the summary request must carry none — while the live history keeps them.
func TestBuildSummaryRequestLeavesThinkingOut(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		userText("fix the bug"),
		{
			Role: anthropic.BetaMessageParamRoleAssistant,
			Content: []anthropic.BetaContentBlockParamUnion{
				anthropic.NewBetaThinkingBlock("sig-1", ""),
				anthropic.NewBetaToolUseBlock("t1", map[string]any{"path": "a.go"}, "Read"),
			},
		},
		toolResult("t1", "package a"),
		{
			// A turn cut off while it was still thinking holds nothing else.
			Role: anthropic.BetaMessageParamRoleAssistant,
			Content: []anthropic.BetaContentBlockParamUnion{
				anthropic.NewBetaRedactedThinkingBlock("opaque"),
			},
		},
	}

	req := BuildSummaryRequest(history, "claude-fable-5-1", 4096)

	if len(req.Messages) != len(history)+1 {
		t.Fatalf("summary request has %d messages, want %d (history + instruction)", len(req.Messages), len(history)+1)
	}
	for i, m := range req.Messages {
		if len(m.Content) == 0 {
			t.Errorf("messages[%d] is empty; the API rejects that", i)
		}
		for j, b := range m.Content {
			if b.OfThinking != nil || b.OfRedactedThinking != nil {
				t.Errorf("messages[%d].content[%d] is a thinking block; the summary request must carry none", i, j)
			}
		}
	}
	if tu := req.Messages[1].Content[0].OfToolUse; tu == nil || tu.ID != "t1" {
		t.Errorf("the tool call must survive the strip, got %+v", req.Messages[1].Content)
	}
	if txt := req.Messages[3].Content[0].OfText; txt == nil || txt.Text != thinkingOmittedPlaceholder {
		t.Errorf("a thinking-only turn should carry the placeholder, got %+v", req.Messages[3].Content)
	}
	if txt := req.Messages[4].Content[0].OfText; txt == nil || txt.Text != SummaryInstruction {
		t.Errorf("the last message should be the summary instruction, got %+v", req.Messages[4].Content)
	}

	// The live history is untouched: its thinking stays for the next turn.
	if history[1].Content[0].OfThinking == nil || history[3].Content[0].OfRedactedThinking == nil {
		t.Error("BuildSummaryRequest mutated the conversation it was given")
	}
}
