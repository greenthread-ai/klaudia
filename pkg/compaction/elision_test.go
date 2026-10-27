package compaction

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// The gap this closes: microcompact replaced an old tool result with a bare
// "[Old tool result elided to save context]" — no preview, no path, no way
// back. The model could not tell what had been dropped, let alone recover it,
// and three elided results were indistinguishable from each other.
func TestElidedResultsNameTheirSpillFile(t *testing.T) {
	big := strings.Repeat("x", 60000)
	var msgs []anthropic.BetaMessageParam
	for i := 0; i < 5; i++ {
		msgs = append(msgs, toolResult("t", big))
	}

	var spilled []string
	spill := func(content string) (string, bool) {
		spilled = append(spilled, content)
		return "/tmp/elided-" + string(rune('a'+len(spilled)-1)) + ".log", true
	}

	out, res := Microcompact(msgs, spill)
	if !res.Compacted {
		t.Fatal("expected compaction")
	}
	if len(spilled) != res.ElidedCount {
		t.Errorf("spilled %d results but elided %d", len(spilled), res.ElidedCount)
	}
	for _, c := range spilled {
		if c != big {
			t.Error("the spill should receive the complete original content")
		}
	}
	for i, p := range elidedPlaceholders(out) {
		if !strings.Contains(p, "/tmp/elided-") {
			t.Errorf("placeholder %d does not name a spill file: %q", i, p)
		}
	}
}

// A placeholder that says nothing is barely better than deleting the result:
// the model cannot tell whether it needs to go and re-read it. The first line
// is cheap and identifies the result.
func TestElidedPlaceholderKeepsAnIdentifyingPreview(t *testing.T) {
	first := "./internal/agent/loop.go:349: for _, tu := range toolUses {"
	body := first + "\n" + strings.Repeat("more matches\n", 6000)
	var msgs []anthropic.BetaMessageParam
	for i := 0; i < 5; i++ {
		msgs = append(msgs, toolResult("t", body))
	}

	out, res := Microcompact(msgs, nil)
	if !res.Compacted {
		t.Fatal("expected compaction")
	}
	ps := elidedPlaceholders(out)
	if len(ps) == 0 {
		t.Fatal("no placeholders found")
	}
	for i, p := range ps {
		if !strings.Contains(p, "loop.go:349") {
			t.Errorf("placeholder %d lost the identifying first line: %q", i, p)
		}
	}
}

// Without a spiller the placeholder must still be well-formed — it just has no
// path to offer. This is the sub-agent and test path.
func TestElisionWorksWithoutASpiller(t *testing.T) {
	big := strings.Repeat("x", 60000)
	var msgs []anthropic.BetaMessageParam
	for i := 0; i < 5; i++ {
		msgs = append(msgs, toolResult("t", big))
	}

	out, res := Microcompact(msgs, nil)
	if !res.Compacted {
		t.Fatal("expected compaction")
	}
	for i, p := range elidedPlaceholders(out) {
		if strings.Contains(p, "full text:") {
			t.Errorf("placeholder %d promises a file that was never written: %q", i, p)
		}
	}
}

// The savings floor is computed against the placeholder, so a placeholder that
// grew without the arithmetic following it would make MinTokensToSave a lie.
func TestSavingsAccountForTheLargerPlaceholder(t *testing.T) {
	big := strings.Repeat("x", 60000)
	var msgs []anthropic.BetaMessageParam
	for i := 0; i < 5; i++ {
		msgs = append(msgs, toolResult("t", big))
	}

	out, res := Microcompact(msgs, func(string) (string, bool) {
		return "/tmp/a-very-long-spill-path-that-costs-real-tokens.log", true
	})
	if !res.Compacted {
		t.Fatal("expected compaction")
	}
	actual := EstimateTokens(msgs) - EstimateTokens(out)
	// Allow a small slack for per-block estimation noise, but the reported
	// figure must not overstate what was actually saved.
	if res.TokensSaved > actual {
		t.Errorf("reported saving %d exceeds the real saving %d", res.TokensSaved, actual)
	}
}

// elidedPlaceholders returns the text of every tool_result that microcompact
// replaced.
func elidedPlaceholders(msgs []anthropic.BetaMessageParam) []string {
	var out []string
	for _, m := range msgs {
		for _, b := range m.Content {
			tr := b.OfToolResult
			if tr == nil {
				continue
			}
			for _, c := range tr.Content {
				if c.OfText != nil && strings.Contains(c.OfText.Text, "elided to save context") {
					out = append(out, c.OfText.Text)
				}
			}
		}
	}
	return out
}

// Every turn re-elided every earlier placeholder: a fresh spill file each, a
// placeholder wrapped around a placeholder, and the lot reported as new. The
// floor was cleared by pricing new placeholders without their path while the
// old ones had one, which only adds up past ~1,180 elisions — so it appeared
// only in very long sessions (measured: 1,208, 1,209, 1,210 "elided" on
// successive turns, saving ~-22 tokens, ~1,200 files written per turn).
func TestMicrocompactLeavesItsOwnPlaceholdersAlone(t *testing.T) {
	spillPath := func(n int) string {
		return "/Users/someone/.klaudia/outputs/elided-" + strings.Repeat("7", 10) + ".log"
	}
	tests := []struct {
		name        string
		placeholder int // already-elided results, as a previous pass left them
		fresh       int // new results big enough to be worth eliding
		wantElided  int
	}{
		{name: "a long session with nothing new is left alone", placeholder: 1300, fresh: 0, wantElided: 0},
		{name: "only the genuinely new results are elided", placeholder: 1300, fresh: 3, wantElided: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var msgs []anthropic.BetaMessageParam
			for i := 0; i < tc.placeholder; i++ {
				msgs = append(msgs, toolResult("t", elidedPlaceholder(strings.Repeat("y", 250), spillPath(i))))
			}
			for i := 0; i < tc.fresh; i++ {
				msgs = append(msgs, toolResult("t", strings.Repeat("x", 60000)))
			}
			// The most recent results are kept verbatim, and are big enough
			// that the tool-result total clears the threshold.
			for i := 0; i < KeepLastNResults; i++ {
				msgs = append(msgs, toolResult("t", strings.Repeat("z", 60000)))
			}

			spilled := 0
			out, res := Microcompact(msgs, func(string) (string, bool) { spilled++; return spillPath(spilled), true })

			if res.ElidedCount != tc.wantElided {
				t.Errorf("elided %d, want %d", res.ElidedCount, tc.wantElided)
			}
			if spilled != tc.wantElided {
				t.Errorf("wrote %d spill files, want %d", spilled, tc.wantElided)
			}
			if res.Compacted && res.TokensSaved <= 0 {
				t.Errorf("reported saving %d; a pass that saves nothing must not run", res.TokensSaved)
			}
			for _, p := range elidedPlaceholders(out) {
				if strings.Count(p, elidedMarker) > 1 {
					t.Fatalf("a placeholder was wrapped in another: %.160s", p)
				}
			}
		})
	}
}
