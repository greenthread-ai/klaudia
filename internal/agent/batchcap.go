package agent

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// maxMessageOutput caps the combined text of one turn's tool results.
//
// The per-result cap is not enough on its own, and parallel dispatch is what
// makes that obvious: five calls that each stop just under the 30 KB budget are
// 150 KB in a single user message, and a turn is not limited to five. Twenty
// Greps, each individually well-behaved, is most of a 200K window in one
// message — and unlike a single gusher, nothing in the per-result path sees
// anything wrong.
//
// 200 KB is roughly 50K tokens, a quarter of the default window. It is chosen
// to be generous enough that an ordinary turn never reaches it: the cap exists
// for the pathological batch, and a cap that trims normal work would cost more
// than it saves.
const maxMessageOutput = 200000

// capMessage trims a turn's tool results so their combined text fits
// maxMessageOutput, and reports whether it changed anything.
//
// Shares are allotted by water-filling rather than evenly: results already
// under their share keep everything, and what they do not use is redistributed
// to the large ones. Trimming a 200-byte result to an equal share of a budget
// it was never going to exhaust would lose real content to no purpose, and the
// bytes that need removing are all in the few big results anyway.
//
// It runs after the per-call events have been emitted, so a local frontend
// still shows what each tool actually produced. Only the model-facing copy is
// trimmed, and the notice naming the spill file travels with it.
func capMessage(blocks []anthropic.BetaContentBlockParamUnion, names []string, spill tools.SpillStore) bool {
	texts := make([]string, len(blocks))
	total := 0
	for i, b := range blocks {
		texts[i] = resultBlockText(b)
		total += len(texts[i])
	}
	if total <= maxMessageOutput {
		return false
	}

	sizes := make([]int, len(texts))
	for i, s := range texts {
		sizes[i] = len(s)
	}
	budgets := fairShares(sizes, maxMessageOutput)

	changed := false
	for i := range blocks {
		if sizes[i] <= budgets[i] {
			continue
		}
		name := ""
		if i < len(names) {
			name = names[i]
		}
		capped, cut := spill.CapTo(name, texts[i], budgets[i])
		if !cut {
			continue
		}
		setResultBlockText(&blocks[i], capped)
		changed = true
	}
	return changed
}

// fairShares distributes total across sizes so that no item gets more than it
// needs and the remainder goes to the items that do. Items are considered
// smallest-first, each taking either its full size or an equal split of what
// is left among the items not yet settled.
func fairShares(sizes []int, total int) []int {
	order := make([]int, len(sizes))
	for i := range order {
		order[i] = i
	}
	// Insertion sort by size: these slices are the length of one turn's tool
	// calls, so this is never the expensive part.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && sizes[order[j]] < sizes[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}

	budgets := make([]int, len(sizes))
	remaining, left := total, len(order)
	for _, i := range order {
		share := remaining / left
		if sizes[i] <= share {
			budgets[i] = sizes[i]
		} else {
			budgets[i] = share
		}
		remaining -= budgets[i]
		left--
	}
	return budgets
}

// resultBlockText concatenates the text parts of a tool_result block, leaving
// image parts alone.
func resultBlockText(b anthropic.BetaContentBlockParamUnion) string {
	tr := b.OfToolResult
	if tr == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range tr.Content {
		if c.OfText != nil {
			sb.WriteString(c.OfText.Text)
		}
	}
	return sb.String()
}

// setResultBlockText replaces a block's text with s, collapsing multiple text
// parts into one and keeping every image part in place.
func setResultBlockText(b *anthropic.BetaContentBlockParamUnion, s string) {
	tr := b.OfToolResult
	if tr == nil {
		return
	}
	out := make([]anthropic.BetaToolResultBlockParamContentUnion, 0, len(tr.Content))
	written := false
	for _, c := range tr.Content {
		if c.OfText != nil {
			if written {
				continue
			}
			out = append(out, anthropic.BetaToolResultBlockParamContentUnion{
				OfText: &anthropic.BetaTextBlockParam{Text: s},
			})
			written = true
			continue
		}
		out = append(out, c)
	}
	if !written {
		out = append(out, anthropic.BetaToolResultBlockParamContentUnion{
			OfText: &anthropic.BetaTextBlockParam{Text: s},
		})
	}
	tr.Content = out
}
