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
		// The conversation moved on, which is what makes the call abandoned
		// rather than pending: the user came back and asked something else.
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("never mind, do this instead")),
	}

	if !needsSanitize(msgs) {
		t.Fatal("needsSanitize missed the orphan server_tool_use — the repair would never run")
	}
	out := sanitizeMessages(msgs)

	for _, m := range out {
		if len(unpairedServerToolIDs(m.Content, false)) > 0 {
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
	if len(unpairedServerToolIDs(content, false)) != 0 {
		t.Error("a non-web server tool was wrongly treated as an orphan")
	}
}

// Several searches in one turn: only the unanswered one goes.
func TestOrphanRepairIsPerToolUseID(t *testing.T) {
	content := []anthropic.BetaContentBlockParamUnion{
		webSearchUse("done"), webSearchResult("done"),
		webSearchUse("cut_off"),
	}
	orphans := unpairedServerToolIDs(content, false)
	if len(orphans) != 1 || !orphans["cut_off"] {
		t.Errorf("orphans = %v, want only cut_off", orphans)
	}
}

// The regression this file's first fix caused, found in a real session.
//
// A paused server tool (stop_reason pause_turn) puts its server_tool_use in one
// assistant message and its result in the NEXT one. Sanitize merges same-role
// runs, so together they are a valid pair — but only after the merge. Repairing
// before it read the first half as an orphan, dropped it, and left the result
// with no preceding call, which the API rejects just as hard:
//
//	unexpected `tool_use_id` found in `web_search_tool_result` blocks: … Each
//	`web_search_tool_result` block must have a corresponding `server_tool_use`
//	block before it
//
// The pair must survive intact.
func TestSanitizeKeepsAPairSplitAcrossPauseTurnMessages(t *testing.T) {
	const id = "srvtoolu_01AHPyWiFdHX2DGhPwEjctdo"
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("research this")),
		// pause_turn: the call, recorded before its result existed.
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock("Searching."), webSearchUse(id),
		}},
		// the continuation, carrying the result for that same call
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchResult(id), anthropic.NewBetaTextBlock("Here's what I found."),
		}},
	}

	out := sanitizeMessages(msgs)
	for _, m := range out {
		if bad := unpairedServerToolIDs(m.Content, false); len(bad) > 0 {
			t.Fatalf("sanitize broke a valid pause_turn pair: %v", bad)
		}
	}
	var sawCall, sawResult bool
	for _, m := range out {
		for _, b := range m.Content {
			if su := b.OfServerToolUse; su != nil && su.ID == id {
				sawCall = true
			}
			if rid, ok := serverToolResultID(b); ok && rid == id {
				sawResult = true
			}
		}
	}
	if !sawCall || !sawResult {
		t.Errorf("the pair did not survive: call=%v result=%v", sawCall, sawResult)
	}
}

// The mirror shape: a result whose call is gone. Dropping it is the only repair
// available — the call cannot be invented — and it must not be left to 400.
func TestSanitizeDropsResultWithNoCall(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("news?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchResult("srvtoolu_lost"), anthropic.NewBetaTextBlock("Based on that…"),
		}},
	}
	if !needsSanitize(msgs) {
		t.Fatal("needsSanitize missed a result with no call")
	}
	out := sanitizeMessages(msgs)
	for _, m := range out {
		for _, b := range m.Content {
			if _, ok := serverToolResultID(b); ok {
				t.Error("unpaired result survived; the request would still 400")
			}
		}
	}
}

// Ordering counts: the call must come BEFORE its result. A result that precedes
// its call is rejected, so the pair is dropped rather than reordered.
func TestResultBeforeItsCallIsUnpaired(t *testing.T) {
	content := []anthropic.BetaContentBlockParamUnion{
		webSearchResult("backwards"), webSearchUse("backwards"),
	}
	if bad := unpairedServerToolIDs(content, false); !bad["backwards"] {
		t.Errorf("a result preceding its call was accepted: %v", bad)
	}
}

// The second regression this repair caused, from a real session.
//
// When the model calls a server tool and a client tool in the same batch, the
// API does NOT run the server tool: it returns stop_reason "tool_use", we return
// the client results, and it runs the search on the next request. So the
// server_tool_use sits unanswered *on purpose*. Reading that as an orphan and
// deleting it threw away a search the model had asked for, and the very next
// request failed.
//
// Shape taken from the transcript that broke: an assistant turn holding
// thinking + text + a client tool_use + the pending web_search, answered by a
// tool_result message carrying only the client tool's result.
func TestSanitizeKeepsAPendingParallelServerToolCall(t *testing.T) {
	const (
		clientID = "toolu_memory"
		searchID = "srvtoolu_pending"
	)
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("get slurm running locally")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock("Let me check memory and research the release."),
			{OfToolUse: &anthropic.BetaToolUseBlockParam{ID: clientID, Name: "Memory", Input: map[string]any{}}},
			webSearchUse(searchID), // withheld by the API until the client result returns
		}},
		anthropic.NewBetaUserMessage(
			anthropic.NewBetaToolResultBlock(clientID, "No memories matched.", false)),
	}

	out := sanitizeMessages(msgs)
	var kept bool
	for _, m := range out {
		for _, b := range m.Content {
			if su := b.OfServerToolUse; su != nil && su.ID == searchID {
				kept = true
			}
		}
	}
	if !kept {
		t.Error("the pending web_search was deleted; the model's search is silently lost")
	}
}

// Once the conversation moves past that turn, the same shape IS abandoned: the
// following user message is ordinary text, not the tool_result answering it.
func TestPendingOnlyAppliesWhileTheTurnIsStillBeingAnswered(t *testing.T) {
	assistant := anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{webSearchUse("srvtoolu_x")},
	}
	midTurn := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("q")), assistant,
		anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("toolu_1", "ok", false)),
	}
	if got := pendingAssistantIndex(midTurn); got != 1 {
		t.Errorf("mid-turn: pending index = %d, want 1", got)
	}
	movedOn := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("q")), assistant,
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("never mind")),
	}
	if got := pendingAssistantIndex(movedOn); got != -1 {
		t.Errorf("moved on: pending index = %d, want -1 (abandoned)", got)
	}
}
