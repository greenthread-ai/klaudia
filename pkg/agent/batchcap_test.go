package agent

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

func resultBlocksOf(texts ...string) []anthropic.BetaContentBlockParamUnion {
	out := make([]anthropic.BetaContentBlockParamUnion, len(texts))
	for i, s := range texts {
		out[i] = anthropic.NewBetaToolResultBlock("t", s, false)
	}
	return out
}

func totalText(blocks []anthropic.BetaContentBlockParamUnion) int {
	n := 0
	for _, b := range blocks {
		n += len(resultBlockText(b))
	}
	return n
}

func repeatLines(bytes int) string {
	const line = "a line of output of some plausible length for a tool to print\n"
	return strings.Repeat(line, bytes/len(line)+1)
}

// The gap the per-result cap cannot see: every one of these results is inside
// the 30 KB per-result budget, so nothing in dispatch finds anything wrong, and
// together they are well over a quarter of the default context window in a
// single user message. Parallel dispatch makes the shape common rather than
// theoretical.
func TestOneTurnsResultsAreCappedInAggregate(t *testing.T) {
	spill := tools.SpillStore{Dir: t.TempDir()}

	const each = 28000
	texts := make([]string, 10)
	names := make([]string, len(texts))
	for i := range texts {
		texts[i] = repeatLines(each)
		names[i] = "Grep"
	}
	blocks := resultBlocksOf(texts...)

	before := totalText(blocks)
	if before <= maxMessageOutput {
		t.Fatalf("the fixture does not exceed the budget: %d bytes", before)
	}

	if !capMessage(blocks, names, spill) {
		t.Fatal("capMessage reported no change")
	}
	after := totalText(blocks)
	// Each trimmed result gains an elision marker and a line naming its spill
	// file, so the budget is met to within that per-block overhead.
	if limit := maxMessageOutput + 500*len(blocks); after > limit {
		t.Errorf("combined results = %d bytes, want at most %d", after, limit)
	}
	if after >= before {
		t.Errorf("nothing was trimmed: %d bytes before, %d after", before, after)
	}
	for i, b := range blocks {
		if !strings.Contains(resultBlockText(b), "bytes elided") {
			t.Errorf("result %d was trimmed without saying so", i)
		}
	}
}

// Water-filling, not an equal split: a result that was never going to exhaust
// its share keeps every byte, because trimming it would lose real content
// without freeing the bytes that are actually the problem.
func TestSmallResultsSurviveTheAggregateCap(t *testing.T) {
	spill := tools.SpillStore{Dir: t.TempDir()}

	small := "ok\n"
	texts := []string{small, repeatLines(maxMessageOutput * 2), small, small}
	names := []string{"Read", "Grep", "Read", "Read"}
	blocks := resultBlocksOf(texts...)

	capMessage(blocks, names, spill)

	for _, i := range []int{0, 2, 3} {
		if got := resultBlockText(blocks[i]); got != small {
			t.Errorf("small result %d was trimmed to %q", i, got)
		}
	}
	if got := resultBlockText(blocks[1]); len(got) >= len(texts[1]) {
		t.Error("the large result was not trimmed")
	}
}

// An ordinary turn must come through untouched. The cap exists for the
// pathological batch; one that trims normal work would cost more than it saves.
func TestAggregateCapLeavesAnOrdinaryTurnAlone(t *testing.T) {
	texts := []string{repeatLines(4000), "ok\n", repeatLines(12000)}
	blocks := resultBlocksOf(texts...)

	if capMessage(blocks, []string{"Read", "Read", "Grep"}, tools.SpillStore{}) {
		t.Error("capMessage trimmed a turn that was inside the budget")
	}
	for i, b := range blocks {
		if resultBlockText(b) != texts[i] {
			t.Errorf("result %d was modified", i)
		}
	}
}

func TestFairSharesRedistributesUnusedBudget(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sizes []int
		total int
		want  []int
	}{
		{"all fit", []int{10, 20, 30}, 100, []int{10, 20, 30}},
		{"equal and over", []int{100, 100}, 100, []int{50, 50}},
		{
			// 10 goes to the small one; the remaining 90 splits between the two
			// that need it, rather than all three taking 33 and the small one
			// wasting 23 of it.
			"one small frees budget for the rest",
			[]int{10, 500, 500}, 100, []int{10, 45, 45},
		},
		{"single result takes the lot", []int{1000}, 100, []int{100}},
		{"nothing to share", []int{}, 100, []int{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := fairShares(tc.sizes, tc.total)
			if len(got) != len(tc.want) {
				t.Fatalf("fairShares(%v, %d) = %v, want %v", tc.sizes, tc.total, got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("fairShares(%v, %d) = %v, want %v", tc.sizes, tc.total, got, tc.want)
				}
			}
		})
	}
}

// Images are vision content the model cannot recover from a log file, and the
// trim is on text only, so they must survive it.
func TestAggregateCapKeepsImageBlocks(t *testing.T) {
	spill := tools.SpillStore{Dir: t.TempDir()}

	blocks := []anthropic.BetaContentBlockParamUnion{
		toolResultWithImages("t1", repeatLines(maxMessageOutput*2), false,
			[]tools.ResultImage{{MediaType: "image/png", Base64: "iVBOR"}}),
	}
	capMessage(blocks, []string{"Read"}, spill)

	tr := blocks[0].OfToolResult
	images := 0
	for _, c := range tr.Content {
		if c.OfImage != nil {
			images++
		}
	}
	if images != 1 {
		t.Errorf("got %d image parts, want 1", images)
	}
	if !strings.Contains(resultBlockText(blocks[0]), "bytes elided") {
		t.Error("the text part was not trimmed")
	}
}

// A result the per-result cap already spilled keeps pointing at that file. A
// second spill would only preserve the already-clamped copy, which is strictly
// worse than the original the first notice names.
func TestAlreadySpilledResultsKeepTheirOriginalNotice(t *testing.T) {
	spill := tools.SpillStore{Dir: t.TempDir()}

	text := repeatLines(maxMessageOutput*2) + "\n[full output: /tmp/first-spill.log]"
	blocks := resultBlocksOf(text)
	capMessage(blocks, []string{"Grep"}, spill)

	got := resultBlockText(blocks[0])
	if !strings.Contains(got, "/tmp/first-spill.log") {
		t.Errorf("the original spill path was lost:\n%s", got[max(0, len(got)-300):])
	}
	if n := strings.Count(got, "[full output: "); n != 1 {
		t.Errorf("got %d spill notices, want 1", n)
	}
}
