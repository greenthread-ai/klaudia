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

func TestCalibrationCorrectsUnderestimate(t *testing.T) {
	var c Calibration
	// Uncalibrated, the estimate passes through — turn one behaves as before.
	if got := c.Scale(1000); got != 1000 {
		t.Fatalf("uncalibrated Scale = %d, want 1000", got)
	}

	// The real case: the estimate saw only the messages, the request also
	// carried the system prompt and every tool schema.
	c.Observe(900_000, 1_000_464)
	if r := c.Ratio(); r < 1.1 {
		t.Fatalf("ratio = %v, want the correction to be material", r)
	}
	if got := c.Scale(900_000); got < 1_000_000 {
		t.Errorf("Scale = %d, should reach the reported size", got)
	}

	// A cached turn reports a small input; that must not undo the correction.
	before := c.Ratio()
	c.Observe(900_000, 10_000)
	if c.Ratio() != before {
		t.Errorf("ratio dropped to %v after a cached turn (was %v)", c.Ratio(), before)
	}
}

func TestCalibrationIgnoresNonsenseAndClamps(t *testing.T) {
	var c Calibration
	c.Observe(0, 500)
	c.Observe(500, 0)
	if c.Ratio() != 1 {
		t.Errorf("zero observations should not calibrate, ratio = %v", c.Ratio())
	}

	c.Observe(10, 10_000_000) // one absurd turn
	if c.Ratio() > maxCalibrationRatio {
		t.Errorf("ratio = %v exceeds the clamp %v", c.Ratio(), maxCalibrationRatio)
	}
}

// The threshold that let the reported failure through: 1M window, autocompact
// at 967k, an estimate that ran ~10% low.
func TestCalibratedEstimateTripsThresholdThatWasMissed(t *testing.T) {
	const window = 1_000_000
	estimate := 950_000 // under the 967k threshold, so nothing fired
	if ShouldAutocompact(estimate, window) {
		t.Fatal("precondition: the raw estimate is below the threshold")
	}

	var c Calibration
	c.Observe(900_000, 1_000_464) // what the API said about the previous turn
	if !ShouldAutocompact(c.Scale(estimate), window) {
		t.Errorf("calibrated estimate %d should trip the threshold", c.Scale(estimate))
	}
}
