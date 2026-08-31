package agent

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func webSearchUse(id string) anthropic.BetaContentBlockParamUnion {
	return anthropic.NewBetaServerToolUseBlock(id, map[string]any{"query": "anything"},
		anthropic.BetaServerToolUseBlockParamNameWebSearch)
}

func webSearchResult(id string) anthropic.BetaContentBlockParamUnion {
	return anthropic.BetaContentBlockParamUnion{
		OfWebSearchToolResult: &anthropic.BetaWebSearchToolResultBlockParam{
			ToolUseID: id,
			Content: anthropic.BetaWebSearchToolResultBlockParamContentUnion{
				OfResultBlock: []anthropic.BetaWebSearchResultBlockParam{{
					URL: "https://example.com", Title: "Example", EncryptedContent: "enc",
				}},
			},
		},
	}
}

// The reported break: a turn cancelled while a web search was in flight recorded
// the assistant message carrying the server_tool_use with no
// web_search_tool_result. Every later request then died with
//
//	messages.17: `web_search` tool use with id `srvtoolu_…` was found without a
//	corresponding `web_search_tool_result` block
//
// leaving the session unusable — it cannot be continued or resumed. Unlike a
// client tool_use, a server tool's result belongs to the SAME assistant message,
// so the orphan-tool_use repair never covered this shape.
func TestSanitizeDropsOrphanServerToolUse(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("what's the news?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock("Let me search."),
			webSearchUse("srvtoolu_018gw2qiARDsZNG4iCuMLzGu"), // never answered
		}},
	}

	if !needsSanitize(msgs) {
		t.Fatal("needsSanitize missed the orphan server_tool_use — the repair would never run")
	}
	out := sanitizeMessages(msgs)

	for _, m := range out {
		if len(orphanServerToolUseIDs(m.Content)) > 0 {
			t.Error("orphan server_tool_use survived sanitize; the request would still 400")
		}
		for _, b := range m.Content {
			if su := b.OfServerToolUse; su != nil {
				t.Errorf("unpaired server_tool_use %q was not dropped", su.ID)
			}
		}
	}
	// The model is told, rather than the exchange vanishing silently.
	var text string
	for _, m := range out {
		for _, b := range m.Content {
			if b.OfText != nil {
				text += b.OfText.Text
			}
		}
	}
	if !strings.Contains(text, "could not be restored") && !strings.Contains(text, "interrupted") {
		t.Errorf("no note explaining the dropped search:\n%s", text)
	}
	// The surrounding text is preserved.
	if !strings.Contains(text, "Let me search.") {
		t.Error("unrelated content was dropped along with the orphan")
	}
}

// A completed search must be left exactly as it is: dropping it would discard
// real results and break the citations that reference them.
func TestSanitizeKeepsPairedServerToolUse(t *testing.T) {
	const id = "srvtoolu_ok"
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("news?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse(id), webSearchResult(id),
		}},
	}
	if needsSanitize(msgs) {
		t.Fatal("a correctly paired web search was flagged as broken")
	}
	if got := sanitizeMessages(msgs); len(got[1].Content) != 2 {
		t.Errorf("paired search was modified: %d blocks, want 2", len(got[1].Content))
	}
}

// Only the server tools we enable are repaired. Another server tool answers with
// a result block this package doesn't model, so an unpaired one is not evidence
// of corruption and must not be dropped.
func TestOrphanRepairIgnoresOtherServerTools(t *testing.T) {
	content := []anthropic.BetaContentBlockParamUnion{
		anthropic.NewBetaServerToolUseBlock("srvtoolu_code", map[string]any{"code": "1+1"},
			anthropic.BetaServerToolUseBlockParamNameCodeExecution),
	}
	if len(orphanServerToolUseIDs(content)) != 0 {
		t.Error("a non-web server tool was wrongly treated as an orphan")
	}
}

// Several searches in one turn: only the unanswered one goes.
func TestOrphanRepairIsPerToolUseID(t *testing.T) {
	content := []anthropic.BetaContentBlockParamUnion{
		webSearchUse("done"), webSearchResult("done"),
		webSearchUse("cut_off"),
	}
	orphans := orphanServerToolUseIDs(content)
	if len(orphans) != 1 || !orphans["cut_off"] {
		t.Errorf("orphans = %v, want only cut_off", orphans)
	}
}
