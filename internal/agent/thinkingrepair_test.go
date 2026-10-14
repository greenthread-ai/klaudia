package agent

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func thinkingBlock(text string) anthropic.BetaContentBlockParamUnion {
	return anthropic.NewBetaThinkingBlock("sig-"+text, text)
}

// Reported from a live session, as a 400 repeating on every send:
//
//	messages.1.content.42: `thinking` or `redacted_thinking` blocks in the
//	latest assistant message cannot be modified.
//
// An interrupted web fetch leaves an unpaired server_tool_use in the last
// assistant message. Editing that message to drop it is exactly what the API
// forbids when the message also carries thinking — so the repair for one 400
// produced another, with no way forward.
func TestTrailingThinkingTurnIsDroppedNotEdited(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("find the schema")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			thinkingBlock("let me look that up"),
			anthropic.NewBetaTextBlock("Searching."),
			webSearchUse("srvtoolu_interrupted"),
		}},
		// The user typed "continue" after the interruption, which is what
		// makes the paused turn non-resumable and sends it through repair.
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("continue")),
	}

	got := sanitizeMessages(msgs)

	for _, m := range got {
		for _, b := range m.Content {
			if su := b.OfServerToolUse; su != nil && su.ID == "srvtoolu_interrupted" {
				t.Fatal("the orphaned server tool survived sanitising")
			}
			if b.OfThinking != nil && len(m.Content) != 3 {
				t.Fatal("the protected message was edited rather than dropped")
			}
		}
	}
	// Both user turns survive, merged, with the broken assistant turn gone.
	if len(got) != 1 || got[0].Role != anthropic.BetaMessageParamRoleUser {
		t.Fatalf("want the user turns left to retry, got %d messages: %+v", len(got), got)
	}
}

// Dropping the turn must not strand tool_results that answered it — those
// would dangle and earn a 400 of their own, turning one dead end into another.
func TestDroppedTurnTakesItsToolResults(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("go")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			thinkingBlock("planning"),
			anthropic.NewBetaToolUseBlock("toolu_1", map[string]string{"command": "ls"}, "Bash"),
			webSearchUse("srvtoolu_interrupted"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("toolu_1", "files", false)),
	}

	for _, m := range DropLastAssistantTurn(msgs) {
		for _, b := range m.Content {
			if tr := b.OfToolResult; tr != nil && tr.ToolUseID == "toolu_1" {
				t.Fatal("tool_result left behind after its tool_use was dropped")
			}
		}
	}
}

// A thinking turn that needs no repair must be left exactly alone.
func TestHealthyThinkingTurnUntouched(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			thinkingBlock("considering"),
			anthropic.NewBetaTextBlock("Hello."),
		}},
	}
	got := sanitizeMessages(msgs)
	if len(got) != 2 || len(got[1].Content) != 2 {
		t.Fatalf("a clean conversation was altered: %+v", got)
	}
}
