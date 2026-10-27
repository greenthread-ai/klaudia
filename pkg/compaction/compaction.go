// Package compaction manages conversation length to stay within the model's
// context window, mirroring the JS two-stage scheme (docs/compaction.md):
//
//   - Microcompact: fast, local, no model call. Elides old tool results when
//     they dominate the context.
//   - Autocompact: model-based summarization when nearing the window limit.
//
// Token counts here are estimates (the JS uses a real tokenizer); they are
// close enough to drive the same thresholds.
package compaction

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/anthropics/anthropic-sdk-go"
)

// Microcompact constants (05-app-core.js).
const (
	KeepLastNResults         = 3
	ToolResultTokenThreshold = 40000
	MinTokensToSave          = 20000
	EstimatedTokensPerImage  = 2000
)

// Autocompact threshold offsets (05-app-core.js calculateTokenThresholds).
const (
	MaxReservedTokens   = 20000
	CompactBufferTokens = 13000
	BlockingLimitOffset = 3000
)

// DefaultContextWindow is the assumed window when the model's is unknown.
const DefaultContextWindow = 200000

// Spiller writes an elided tool result somewhere durable and returns a path
// the model can read. It is injected rather than imported so this package
// stays dependency-free; the agent loop supplies the same on-disk spill the
// per-result cap uses. A nil Spiller means "nowhere to put it", and the
// placeholder then promises no file.
type Spiller func(content string) (path string, ok bool)

// elidedPreviewBytes is how much of the original result survives in the
// placeholder. Enough to identify which call it was — a grep's first match, a
// test run's first line — and not enough to matter against what is saved.
const elidedPreviewBytes = 120

// elidedPlaceholder builds the text that replaces an old tool result.
//
// It used to be a single fixed string, which saved the most tokens possible
// and told the model nothing: three elided results were indistinguishable
// from each other, and there was no way to recover any of them. A result the
// model cannot identify is one it must either re-run or reason without. The
// first line and a path cost a few dozen tokens against the thousands this
// reclaims.
func elidedPlaceholder(content, path string) string {
	var b strings.Builder
	b.WriteString(elidedMarker)
	if n := len(content); n > 0 {
		fmt.Fprintf(&b, "; %d bytes", n)
	}
	if p := firstLine(content, elidedPreviewBytes); p != "" {
		b.WriteString("; began: ")
		b.WriteString(p)
	}
	if path != "" {
		b.WriteString("; full text: ")
		b.WriteString(path)
	}
	b.WriteString("]")
	return b.String()
}

// firstLine returns the first non-empty line of s, truncated to max bytes on a
// rune boundary.
func firstLine(s string, max int) string {
	for _, ln := range strings.SplitN(s, "\n", 4) {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if len(ln) > max {
			ln = ln[:max]
			for len(ln) > 0 && !utf8.ValidString(ln) {
				ln = ln[:len(ln)-1]
			}
			ln += "…"
		}
		return ln
	}
	return ""
}

// EstimateTokens approximates the token count of a message list. Text is
// charged at ~4 chars/token; images/documents at a flat per-item estimate.
func EstimateTokens(messages []anthropic.BetaMessageParam) int {
	total := 0
	for _, m := range messages {
		for _, b := range m.Content {
			total += blockTokens(b)
		}
	}
	return total
}

func blockTokens(b anthropic.BetaContentBlockParamUnion) int {
	switch {
	case b.OfText != nil:
		return charTokens(b.OfText.Text)
	case b.OfToolUse != nil:
		// Rough: the serialized input.
		return charTokens(b.OfToolUse.Name) + 50
	case b.OfToolResult != nil:
		return toolResultTokens(b.OfToolResult)
	case b.OfImage != nil, b.OfDocument != nil:
		return EstimatedTokensPerImage
	case b.OfThinking != nil:
		return charTokens(b.OfThinking.Thinking)
	default:
		return 0
	}
}

func toolResultTokens(tr *anthropic.BetaToolResultBlockParam) int {
	n := 0
	for _, c := range tr.Content {
		if c.OfText != nil {
			n += charTokens(c.OfText.Text)
		} else {
			n += EstimatedTokensPerImage
		}
	}
	return n
}

func charTokens(s string) int { return (len(s) + 3) / 4 }

// Result reports what a microcompact pass did.
type Result struct {
	Compacted   bool
	TokensSaved int
	ElidedCount int
}

// Microcompact elides the content of tool results older than the most recent
// KeepLastNResults, but only when tool-result tokens exceed the threshold and
// the savings clear MinTokensToSave. It never calls the model. The returned
// slice is a new slice; inputs are not mutated.
//
// spill, when non-nil, receives each elided result's full text and returns a
// path named in the placeholder, so an elision is recoverable rather than a
// deletion. See Spiller.
func Microcompact(messages []anthropic.BetaMessageParam, spill Spiller) ([]anthropic.BetaMessageParam, Result) {
	// Locate every tool_result block as (msgIdx, blockIdx).
	type loc struct{ m, b int }
	var locs []loc
	totalToolTokens := 0
	for mi, m := range messages {
		for bi, blk := range m.Content {
			if blk.OfToolResult != nil {
				locs = append(locs, loc{mi, bi})
				totalToolTokens += toolResultTokens(blk.OfToolResult)
			}
		}
	}

	if totalToolTokens <= ToolResultTokenThreshold || len(locs) <= KeepLastNResults {
		return messages, Result{}
	}

	// Candidates to elide: all but the last KeepLastNResults, minus anything a
	// previous pass already elided.
	//
	// Skipping those is load-bearing. Without it every turn re-elided every
	// earlier placeholder: spilled it to a fresh file, wrapped it in another
	// placeholder, and reported the lot as new. While the placeholder was one
	// fixed string that was a no-op the savings floor refused; once it carried
	// a preview and a path it was not. Measured in a long goal session: ~1,200
	// files written per turn, 15,728 of 20,200 spill files holding nothing but
	// an earlier placeholder, and each original reachable only by one hop per
	// turn since it was elided — i.e. lost.
	var elide []loc
	for _, l := range locs[:len(locs)-KeepLastNResults] {
		if !isElided(messages[l.m].Content[l.b].OfToolResult) {
			elide = append(elide, l)
		}
	}
	if len(elide) == 0 {
		return messages, Result{}
	}

	// Two passes, because spilling has a side effect on disk and the floor
	// below can still say no. compact() runs at the top of every turn, so a
	// single pass that spilled first would write a fresh set of files every
	// turn a conversation sat just under MinTokensToSave. Pass one prices each
	// placeholder with a path-sized allowance: pricing it with no path at all
	// understated every placeholder by its path, which on its own was enough
	// to clear the floor once a session had ~1,200 elisions.
	content := make([]string, len(elide))
	saved := 0
	for i, l := range elide {
		tr := messages[l.m].Content[l.b].OfToolResult
		content[i] = toolResultText(tr)
		saved += toolResultTokens(tr) - charTokens(elidedPlaceholder(content[i], spillPathAllowance))
	}
	if saved < MinTokensToSave {
		return messages, Result{}
	}

	// Pass two: committed now, so spill and build the real placeholders. The
	// saving is recomputed rather than adjusted, because each path has its own
	// length and the reported figure feeds a user-visible message.
	replacement := make([]string, len(elide))
	saved = 0
	for i, l := range elide {
		path := ""
		if spill != nil && content[i] != "" {
			if p, ok := spill(content[i]); ok {
				path = p
			}
		}
		replacement[i] = elidedPlaceholder(content[i], path)
		saved += toolResultTokens(messages[l.m].Content[l.b].OfToolResult) - charTokens(replacement[i])
	}

	// Apply: deep-copy the affected messages and replace elided blocks.
	out := make([]anthropic.BetaMessageParam, len(messages))
	copy(out, messages)
	touched := map[int]bool{}
	for i, l := range elide {
		if !touched[l.m] {
			nc := make([]anthropic.BetaContentBlockParamUnion, len(messages[l.m].Content))
			copy(nc, messages[l.m].Content)
			out[l.m].Content = nc
			touched[l.m] = true
		}
		orig := out[l.m].Content[l.b].OfToolResult
		isErr := orig.IsError.Or(false)
		out[l.m].Content[l.b] = anthropic.NewBetaToolResultBlock(orig.ToolUseID, replacement[i], isErr)
	}

	return out, Result{Compacted: true, TokensSaved: saved, ElidedCount: len(elide)}
}

// spillPathAllowance stands in for a spill path when pricing a placeholder
// before anything is spilled. Its length is what matters: a little longer than
// a real ~/.klaudia/outputs/elided-<n>.log path, so the floor errs towards no.
var spillPathAllowance = strings.Repeat("x", 80)

// elidedMarker opens every placeholder elidedPlaceholder builds, old and new.
const elidedMarker = "[Old tool result elided to save context"

// isElided reports whether a tool result is already a microcompact
// placeholder.
func isElided(tr *anthropic.BetaToolResultBlockParam) bool {
	return strings.HasPrefix(toolResultText(tr), elidedMarker)
}

// toolResultText concatenates the text blocks of a tool result. Images carry
// no text and are not recoverable through a spill file, so they are skipped.
func toolResultText(tr *anthropic.BetaToolResultBlockParam) string {
	var b strings.Builder
	for _, c := range tr.Content {
		if c.OfText != nil {
			b.WriteString(c.OfText.Text)
		}
	}
	return b.String()
}

// Thresholds holds the computed autocompact thresholds for a context window.
type Thresholds struct {
	EffectiveWindow  int
	CompactThreshold int // autocompact triggers above this
	BlockingLimit    int // hard stop
}

// ComputeThresholds mirrors calculateTokenThresholds (05-app-core.js:117395).
func ComputeThresholds(contextWindow int) Thresholds {
	if contextWindow <= 0 {
		contextWindow = DefaultContextWindow
	}
	reserve := MaxReservedTokens
	if contextWindow < reserve {
		reserve = contextWindow / 2
	}
	eff := contextWindow - reserve
	return Thresholds{
		EffectiveWindow:  eff,
		CompactThreshold: eff - CompactBufferTokens,
		BlockingLimit:    eff - BlockingLimitOffset,
	}
}

// ShouldAutocompact reports whether the token count has crossed the compact
// threshold for the given context window.
func ShouldAutocompact(tokenCount, contextWindow int) bool {
	return tokenCount > ComputeThresholds(contextWindow).CompactThreshold
}

// Calibration corrects the local estimate against what the API actually
// charged for the request.
//
// EstimateTokens only sees the message list, at ~4 chars/token. A real request
// also carries the system prompt, the project instructions and every tool's
// JSON schema, and code tokenises closer to 3 chars/token — so the estimate
// runs low, and in a large window it runs low by more than the safety buffer.
// That is how a 1M-token session reached "prompt is too long: 1000464 tokens >
// 1000000 maximum" with the 967k autocompact threshold never tripping.
//
// Every response reports the true input size, so after one turn we know the
// ratio between estimate and reality and can scale subsequent estimates by it.
// The ratio only ever grows: a turn whose cache made the reported figure look
// small must not talk us out of a correction we have already earned.
type Calibration struct {
	ratio float64
}

// maxCalibrationRatio bounds the correction, so one anomalous turn (a giant
// image, a pathological schema) cannot make every later estimate absurd.
const maxCalibrationRatio = 4.0

// Observe records an estimate and the reported input tokens for the same
// request. Zero or negative values are ignored.
func (c *Calibration) Observe(estimated, reported int) {
	if estimated <= 0 || reported <= 0 {
		return
	}
	r := float64(reported) / float64(estimated)
	if r > maxCalibrationRatio {
		r = maxCalibrationRatio
	}
	if r > c.ratio {
		c.ratio = r
	}
}

// Scale applies the correction. Before any observation it returns the estimate
// unchanged, so behaviour on turn one is what it always was.
func (c *Calibration) Scale(estimate int) int {
	if c.ratio <= 1 {
		return estimate
	}
	return int(float64(estimate) * c.ratio)
}

// Ratio reports the current correction factor (1 when uncalibrated), for
// diagnostics.
func (c *Calibration) Ratio() float64 {
	if c.ratio <= 1 {
		return 1
	}
	return c.ratio
}
