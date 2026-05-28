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

func TestSanitizeMessagesPassthrough(t *testing.T) {
	// Already-clean conversations must not be re-allocated or rewritten.
	good := anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))
	in := []anthropic.BetaMessageParam{good, good}
	out := sanitizeMessages(in)
	if &out[0] != &in[0] {
		t.Error("clean input should be returned without reallocation")
	}
	// Empty input doesn't panic.
	if got := sanitizeMessages(nil); got != nil {
		t.Errorf("nil input = %#v, want nil", got)
	}
}
