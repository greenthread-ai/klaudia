package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestSanitizeMessagesFillsEmptyContent(t *testing.T) {
	// Matches the shape we found poisoning a resumed transcript: an assistant
	// message recorded with `"content": null` after the OpenAI-compatible shim
	// returned a refusal. Unmarshaled into BetaMessageParam, Content is nil.
	raw := []byte(`{"role":"assistant","content":null,"stop_reason":"end_turn"}`)
	var bad anthropic.BetaMessageParam
	if err := json.Unmarshal(raw, &bad); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(bad.Content) != 0 {
		t.Fatalf("setup: expected empty content from null, got %d block(s)", len(bad.Content))
	}

	good := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("ok"))
	in := []anthropic.BetaMessageParam{good, bad, good}
	out := sanitizeMessages(in)

	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 — sanitization must preserve message count and alternation", len(out))
	}
	if len(out[1].Content) != 1 || out[1].Content[0].OfText == nil {
		t.Errorf("empty-content message not repaired: %#v", out[1].Content)
	}
	if out[1].Content[0].OfText.Text != emptyContentPlaceholder {
		t.Errorf("placeholder text = %q, want %q", out[1].Content[0].OfText.Text, emptyContentPlaceholder)
	}
	// Untouched messages share structure with the input (fast path on writes).
	if len(out[0].Content) != 1 || out[0].Content[0].OfText == nil || out[0].Content[0].OfText.Text != "ok" {
		t.Errorf("non-empty message was mutated: %#v", out[0].Content)
	}
}

// asstMessage builds an assistant BetaMessageParam from content blocks. The SDK
// only ships NewBetaUserMessage; we need the assistant role too in tests.
func asstMessage(blocks ...anthropic.BetaContentBlockParamUnion) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{
		Role:    anthropic.BetaMessageParamRoleAssistant,
		Content: blocks,
	}
}

func TestSanitizeMessagesPassthrough(t *testing.T) {
	// Already-clean alternating conversation must not be re-allocated or rewritten.
	user := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))
	asst := asstMessage(anthropic.NewBetaTextBlock("hello"))
	in := []anthropic.BetaMessageParam{user, asst, user}
	out := sanitizeMessages(in)
	if &out[0] != &in[0] {
		t.Error("clean input should be returned without reallocation")
	}
	// Empty input doesn't panic.
	if got := sanitizeMessages(nil); got != nil {
		t.Errorf("nil input = %#v, want nil", got)
	}
}

func TestSanitizeMessagesRepairsOrphanToolUse(t *testing.T) {
	// Real shape from the corrupted huedoku transcript: an assistant tool_use
	// (Bash) followed by user text messages — no matching tool_result. The API
	// rejects with "tool_use ids were found without tool_result blocks
	// immediately after". Sanitize must inject a synthetic tool_result.
	asstToolUse := asstMessage(
		anthropic.NewBetaToolUseBlock("chatcmpl-tool-90ed3808", map[string]any{"cmd": "ls"}, "Bash"),
	)
	userText := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("iteration prompt"))
	out := sanitizeMessages([]anthropic.BetaMessageParam{asstToolUse, userText})

	if len(out) != 2 {
		t.Fatalf("len = %d, want 2 (synthetic result merged into next user)", len(out))
	}
	// First content of the user message is now a tool_result for that id.
	if tr := out[1].Content[0].OfToolResult; tr == nil || tr.ToolUseID != "chatcmpl-tool-90ed3808" {
		t.Fatalf("missing synthetic tool_result; got %#v", out[1].Content)
	}
	// Original text follows the injected tool_result.
	if len(out[1].Content) != 2 || out[1].Content[1].OfText == nil {
		t.Errorf("original content lost; got %#v", out[1].Content)
	}
}

func TestSanitizeMessagesOrphanToolUseAtEndInsertsUser(t *testing.T) {
	// A trailing assistant tool_use with no following message at all — a new
	// user message carrying the synthetic tool_result is inserted.
	asst := asstMessage(
		anthropic.NewBetaToolUseBlock("orphan-1", map[string]any{}, "Bash"),
	)
	out := sanitizeMessages([]anthropic.BetaMessageParam{asst})
	if len(out) != 2 || out[1].Role != anthropic.BetaMessageParamRoleUser {
		t.Fatalf("expected an injected user message; got %d msgs, last role %v", len(out), out[len(out)-1].Role)
	}
	if tr := out[1].Content[0].OfToolResult; tr == nil || tr.ToolUseID != "orphan-1" {
		t.Errorf("injected message missing the synthetic result; got %#v", out[1].Content)
	}
}

func TestSanitizeMessagesCapsOverLongToolName(t *testing.T) {
	// A tool_use with a name longer than the API limit (a long namespaced MCP
	// tool name is the usual source) 400s the whole request. Cap it to the
	// limit; a normal-length name in the same turn is left alone; and the paired
	// tool_result — which references the call by tool_use_id, not by name — must
	// still match, since the id is never touched.
	longName := strings.Repeat("a", maxToolNameLen+50)
	asst := asstMessage(
		anthropic.NewBetaToolUseBlock("id-long", map[string]any{}, longName),
		anthropic.NewBetaToolUseBlock("id-short", map[string]any{}, "Bash"),
	)
	user := anthropic.NewBetaUserMessage(
		anthropic.NewBetaToolResultBlock("id-long", "ok", false),
		anthropic.NewBetaToolResultBlock("id-short", "ok", false),
	)
	out := sanitizeMessages([]anthropic.BetaMessageParam{asst, user})

	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2 — capping a name must not change the message shape", len(out))
	}
	long := out[0].Content[0].OfToolUse
	if long == nil {
		t.Fatalf("first block is no longer a tool_use: %#v", out[0].Content[0])
	}
	if len(long.Name) != maxToolNameLen {
		t.Errorf("over-long name truncated to %d chars, want %d", len(long.Name), maxToolNameLen)
	}
	if long.Name != longName[:maxToolNameLen] {
		t.Errorf("truncated name = %q, want the first %d chars of the original", long.Name, maxToolNameLen)
	}
	if long.ID != "id-long" {
		t.Errorf("tool_use id was altered: %q, want %q", long.ID, "id-long")
	}
	// A name within the limit is untouched.
	if short := out[0].Content[1].OfToolUse; short == nil || short.Name != "Bash" {
		t.Errorf("normal-length name was rewritten: %#v", out[0].Content[1])
	}
	// The paired tool_result still references the (unchanged) id.
	if tr := out[1].Content[0].OfToolResult; tr == nil || tr.ToolUseID != "id-long" {
		t.Errorf("pairing broke: tool_result = %#v", out[1].Content[0])
	}
}

func TestSanitizeMessagesDropsStaleThinkingButKeepsCurrentTurn(t *testing.T) {
	// After a /model switch, thinking blocks from earlier turns were produced by
	// the previous model and are stale — the new model rejects (or silently
	// drops) them. Sanitize removes thinking from every assistant turn but the
	// in-progress one: a completed prior turn's thinking is never required, while
	// the current turn's may be, so it is preserved.
	u0 := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("start"))
	// a1 is a completed historical turn (followed by ordinary user text).
	a1 := asstMessage(
		anthropic.NewBetaThinkingBlock("sig-old", "stale reasoning"),
		anthropic.NewBetaTextBlock("answer 1"),
	)
	u1 := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("next"))
	// a2 is the in-progress turn: a tool_use answered by the trailing user
	// tool_result, so pendingAssistantIndex points at it.
	a2 := asstMessage(
		anthropic.NewBetaThinkingBlock("sig-cur", "current reasoning"),
		anthropic.NewBetaToolUseBlock("id-x", map[string]any{"cmd": "ls"}, "Bash"),
	)
	u2 := anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("id-x", "ok", false))

	out := sanitizeMessages([]anthropic.BetaMessageParam{u0, a1, u1, a2, u2})
	if len(out) != 5 {
		t.Fatalf("len(out) = %d, want 5 — dropping thinking must not change message count", len(out))
	}

	// Historical turn: thinking gone, its text preserved.
	for _, b := range out[1].Content {
		if b.OfThinking != nil || b.OfRedactedThinking != nil {
			t.Fatalf("stale thinking not dropped from historical turn: %#v", out[1].Content)
		}
	}
	if len(out[1].Content) != 1 || out[1].Content[0].OfText == nil || out[1].Content[0].OfText.Text != "answer 1" {
		t.Errorf("historical turn lost its text: %#v", out[1].Content)
	}

	// In-progress turn: thinking kept alongside the tool_use.
	keptThinking := false
	for _, b := range out[3].Content {
		if b.OfThinking != nil {
			keptThinking = true
		}
	}
	if !keptThinking {
		t.Errorf("current turn's thinking was dropped: %#v", out[3].Content)
	}
}

func TestSanitizeMessagesDropsRedactedThinkingFromHistory(t *testing.T) {
	// redacted_thinking is opaque encrypted reasoning and just as model-bound;
	// a historical one is dropped too. The trailing ordinary user text makes the
	// assistant turn history (no pending turn), so it is not preserved.
	asst := asstMessage(
		anthropic.NewBetaRedactedThinkingBlock("encrypted-blob"),
		anthropic.NewBetaTextBlock("done"),
	)
	user := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("thanks"))
	out := sanitizeMessages([]anthropic.BetaMessageParam{asst, user})
	if len(out) != 2 {
		t.Fatalf("len(out) = %d, want 2", len(out))
	}
	for _, b := range out[0].Content {
		if b.OfRedactedThinking != nil {
			t.Fatalf("redacted_thinking not dropped from history: %#v", out[0].Content)
		}
	}
	if len(out[0].Content) != 1 || out[0].Content[0].OfText == nil {
		t.Errorf("assistant text lost with the redacted thinking: %#v", out[0].Content)
	}
}

func TestSanitizeMessagesMergesConsecutiveSameRole(t *testing.T) {
	// Three identical user prompts in a row (from three failed /goal run
	// attempts) — Anthropic requires alternation, so merge into one user with
	// concatenated blocks.
	mk := func(s string) anthropic.BetaMessageParam {
		return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(s))
	}
	asst := asstMessage(anthropic.NewBetaTextBlock("ok"))
	out := sanitizeMessages([]anthropic.BetaMessageParam{asst, mk("a"), mk("b"), mk("c")})
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2 (the three users merged)", len(out))
	}
	if len(out[1].Content) != 3 {
		t.Errorf("merged user should hold 3 blocks; got %d", len(out[1].Content))
	}
}
