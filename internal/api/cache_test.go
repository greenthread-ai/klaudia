package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func sampleParams() *anthropic.BetaMessageNewParams {
	return &anthropic.BetaMessageNewParams{
		System: []anthropic.BetaTextBlockParam{
			{Text: "you are helpful"},
			{Text: "follow the rules"},
		},
		Tools: []anthropic.BetaToolUnionParam{
			{OfTool: &anthropic.BetaToolParam{Name: "Read"}},
			{OfTool: &anthropic.BetaToolParam{Name: "Write"}},
		},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hello")),
		},
	}
}

// countCacheControl marshals the request and counts cache_control breakpoints —
// the same view the API sees on the wire, and what its 4-breakpoint limit counts.
func countCacheControl(t *testing.T, p *anthropic.BetaMessageNewParams) int {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), `"cache_control"`)
}

func TestApplyCacheControlMarksStablePrefixAndTail(t *testing.T) {
	p := sampleParams()
	applyCacheControl(p)

	// Exactly three breakpoints: last tool, last system block, last message tail.
	if got := countCacheControl(t, p); got != 3 {
		t.Fatalf("cache_control breakpoints = %d, want 3", got)
	}
	if p.Tools[1].OfTool.CacheControl.Type == "" {
		t.Error("last tool not marked")
	}
	if p.Tools[0].OfTool.CacheControl.Type != "" {
		t.Error("only the last tool should be marked")
	}
	if p.System[1].CacheControl.Type == "" {
		t.Error("last system block not marked")
	}
	last := p.Messages[0].Content
	if last[len(last)-1].OfText.CacheControl.Type == "" {
		t.Error("last message block not marked")
	}
}

// The loop reuses the same message-block objects across turns. Re-applying must
// stay at three breakpoints (clear-then-set), never accumulate past the API's
// limit of 4.
func TestApplyCacheControlDoesNotAccumulate(t *testing.T) {
	p := sampleParams()
	applyCacheControl(p) // turn 1

	// Turn 2: a new user message is appended to the same slice.
	p.Messages = append(p.Messages,
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("again")))
	applyCacheControl(p)

	if got := countCacheControl(t, p); got != 3 {
		t.Fatalf("after re-apply: breakpoints = %d, want 3 (must not accumulate)", got)
	}
	// The breakpoint moved to the new last message; the previous one is cleared.
	first := p.Messages[0].Content
	if first[len(first)-1].OfText.CacheControl.Type != "" {
		t.Error("stale breakpoint left on the previous last message")
	}
}

// With server-side web tools appended (OfTool == nil), the tools breakpoint must
// land on the last regular tool, not be skipped — otherwise it's a silent no-op.
func TestApplyCacheControlMarksLastRegularTool(t *testing.T) {
	p := sampleParams()
	// Append a server-side tool with no OfTool, as the loop does for web tools.
	p.Tools = append(p.Tools, anthropic.BetaToolUnionParam{
		OfWebSearchTool20250305: &anthropic.BetaWebSearchTool20250305Param{},
	})
	applyCacheControl(p)
	if p.Tools[1].OfTool.CacheControl.Type == "" {
		t.Error("last regular tool not marked when a server-side tool trails it")
	}
}

func TestApplyCacheControlDisabled(t *testing.T) {
	t.Setenv("KLAUDIA_DISABLE_PROMPT_CACHE", "1")
	p := sampleParams()
	applyCacheControl(p)
	if got := countCacheControl(t, p); got != 0 {
		t.Fatalf("disabled: breakpoints = %d, want 0", got)
	}
}

func TestApplyCacheControlEmptyIsSafe(t *testing.T) {
	p := &anthropic.BetaMessageNewParams{}
	applyCacheControl(p) // must not panic on no tools/system/messages
	if got := countCacheControl(t, p); got != 0 {
		t.Fatalf("empty request: breakpoints = %d, want 0", got)
	}
}
