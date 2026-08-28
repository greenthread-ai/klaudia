package agent

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// A web-search response, as the API delivers it: a text block whose citation
// carries the url and encrypted_index the SDK's ToParam then drops.
const webSearchResponseJSON = `{
  "id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-4-8",
  "stop_reason": "end_turn", "usage": {"input_tokens": 1, "output_tokens": 1},
  "content": [{
    "type": "text", "text": "Dolly Parton news.",
    "citations": [{
      "type": "web_search_result_location",
      "url": "https://example.com/dolly",
      "title": "News", "cited_text": "she is fine",
      "encrypted_index": "enc-abc-123"
    }]
  }]
}`

// toParamRepaired must keep the url and encrypted_index that the SDK's plain
// ToParam drops (v1.45.0). Without it the very next request 400s on
// citations.0.web_search_result_location.url.
func TestToParamRepairedKeepsWebSearchCitationURL(t *testing.T) {
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(webSearchResponseJSON), &m); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// Guard: confirm the SDK bug is still present, so this test stays meaningful.
	lossy := m.ToParam()
	if c := lossy.Content[0].OfText.Citations[0].OfWebSearchResultLocation; c != nil && c.URL != "" {
		t.Skip("SDK ToParam no longer drops the citation url; repair may be unnecessary")
	}

	p := toParamRepaired(m)
	cit := p.Content[0].OfText.Citations[0].OfWebSearchResultLocation
	if cit == nil {
		t.Fatal("citation missing after repair")
	}
	if cit.URL != "https://example.com/dolly" {
		t.Errorf("url = %q, want the restored value", cit.URL)
	}
	if cit.EncryptedIndex != "enc-abc-123" {
		t.Errorf("encrypted_index = %q, want the restored value", cit.EncryptedIndex)
	}
}

// History recorded before the fix has an empty-url citation the value can't be
// recovered for. sanitizeMessages must drop just that citation and keep the
// text, so a resume of an old session stops 400ing.
func TestSanitizeDropsEmptyWebSearchCitation(t *testing.T) {
	broken := anthropic.BetaTextCitationParamUnion{
		OfWebSearchResultLocation: &anthropic.BetaCitationWebSearchResultLocationParam{
			CitedText: "cited", // URL and EncryptedIndex empty — the poisoned shape
		},
	}
	tb := &anthropic.BetaTextBlockParam{
		Text:      "Dolly Parton news.",
		Citations: []anthropic.BetaTextCitationParamUnion{broken},
	}
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("what's the news?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{{OfText: tb}}},
	}

	if !needsSanitize(msgs) {
		t.Fatal("needsSanitize did not flag the empty-url citation")
	}
	out := sanitizeMessages(msgs)
	if hasEmptyWebSearchCitation(out) {
		t.Error("empty-url citation survived sanitize")
	}
	got := out[len(out)-1].Content[0].OfText
	if got == nil || got.Text != "Dolly Parton news." {
		t.Fatalf("annotated text was not preserved: %+v", got)
	}
	if len(got.Citations) != 0 {
		t.Errorf("broken citation not dropped: %d remain", len(got.Citations))
	}
}
