package compaction

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
)

func user(text string) anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(text))
}

func assistant(text string) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(text)}}
}

// The older half goes, and the cut lands on a real user turn — never on a
// tool_result, which would be left without its tool_use.
func TestShrinkForSummaryDropsOlderHalfAtUserTurn(t *testing.T) {
	toolUse := anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaToolUseBlock("t1", map[string]any{}, "Bash")}}
	toolResult := anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("t1", "ok", false))
	msgs := []anthropic.BetaMessageParam{
		user("u0"), assistant("a0"), user("u1"), toolUse, toolResult, assistant("a1"), user("u2"), assistant("a2"),
	}
	out, dropped, ok := ShrinkForSummary(msgs)
	if !ok || dropped != 6 || len(out) != 2 || out[0].Content[0].OfText.Text != "u2" {
		t.Fatalf("got ok=%v dropped=%d first=%v; want the cut at u2 (index 6), past the tool_result at 4", ok, dropped, out[0].Content)
	}
}

// One enormous prompt cannot be dropped, so its longest text is halved, head
// and tail kept, on rune boundaries.
func TestShrinkForSummaryTruncatesHugeText(t *testing.T) {
	big := "HEAD" + strings.Repeat("é", 10000) + "TAIL"
	msgs := []anthropic.BetaMessageParam{user(big)}
	out, dropped, ok := ShrinkForSummary(msgs)
	if !ok || dropped != 0 {
		t.Fatalf("ok=%v dropped=%d, want a truncation", ok, dropped)
	}
	got := out[0].Content[0].OfText.Text
	if len(got) >= len(big) || !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") ||
		!strings.Contains(got, "characters removed") || !utf8.ValidString(got) {
		t.Errorf("truncated text: len %d of %d, valid %v, %q…%q", len(got), len(big), utf8.ValidString(got), got[:12], got[len(got)-12:])
	}
	if msgs[0].Content[0].OfText.Text != big {
		t.Error("input was mutated")
	}
}

// Nothing left to drop or cut: the caller must stop rather than retry forever.
func TestShrinkForSummaryGivesUp(t *testing.T) {
	if _, _, ok := ShrinkForSummary([]anthropic.BetaMessageParam{user("hi")}); ok {
		t.Error("a short one-message conversation cannot be shrunk")
	}
}
