package compaction

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func userText(s string) anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(s))
}

func toolResult(id, content string) anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock(id, content, false))
}

func TestEstimateTokens(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{userText(strings.Repeat("a", 400))}
	if got := EstimateTokens(msgs); got < 90 || got > 110 {
		t.Errorf("EstimateTokens = %d, want ~100", got)
	}
}

func TestMicrocompactNoOpWhenSmall(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		userText("hi"),
		toolResult("t1", "small result"),
	}
	out, res := Microcompact(msgs)
	if res.Compacted {
		t.Error("should not compact small conversations")
	}
	if len(out) != len(msgs) {
		t.Error("message count changed unexpectedly")
	}
}

func TestMicrocompactElidesOldResults(t *testing.T) {
	big := strings.Repeat("x", 60000) // ~15k tokens each
	var msgs []anthropic.BetaMessageParam
	// 5 large tool results: total ~75k tokens (> 40k threshold).
	for i := 0; i < 5; i++ {
		msgs = append(msgs, toolResult("t", big))
	}

	before := EstimateTokens(msgs)
	out, res := Microcompact(msgs)
	if !res.Compacted {
		t.Fatalf("expected compaction; tokens before=%d", before)
	}
	// Last 3 kept; first 2 elided.
	if res.ElidedCount != 2 {
		t.Errorf("ElidedCount = %d, want 2", res.ElidedCount)
	}
	after := EstimateTokens(out)
	if after >= before {
		t.Errorf("tokens not reduced: before=%d after=%d", before, after)
	}
	// Inputs must be untouched (new slice semantics).
	if EstimateTokens(msgs) != before {
		t.Error("Microcompact mutated its input")
	}
}

func TestComputeThresholds(t *testing.T) {
	th := ComputeThresholds(200000)
	if th.EffectiveWindow != 180000 {
		t.Errorf("EffectiveWindow = %d, want 180000", th.EffectiveWindow)
	}
	if th.CompactThreshold != 167000 {
		t.Errorf("CompactThreshold = %d, want 167000", th.CompactThreshold)
	}
}

func TestShouldAutocompact(t *testing.T) {
	if ShouldAutocompact(100000, 200000) {
		t.Error("should not compact at 100k/200k")
	}
	if !ShouldAutocompact(170000, 200000) {
		t.Error("should compact at 170k/200k (> 167k threshold)")
	}
}
