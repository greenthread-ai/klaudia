package agent

import (
	"encoding/json"
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
