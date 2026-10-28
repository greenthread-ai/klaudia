package api

import (
	"github.com/anthropics/anthropic-sdk-go"
)

// applyCacheControl places Anthropic prompt-cache breakpoints on the stable,
// reused parts of a request so they aren't re-processed (and re-billed at full
// rate) every turn: the tool definitions, the system prompt, and a rolling
// breakpoint at the end of the conversation. The API reads the longest
// previously-cached prefix, so the end-of-conversation breakpoint written on one
// turn becomes the cache hit for the next.
//
// Breakpoints below the model's minimum cacheable size are a silent no-op, so
// this is safe to apply unconditionally. Tuning.NoPromptCache turns it off.
//
// It clears any pre-existing breakpoints first: message content blocks are the
// same objects turn after turn (the loop reuses the history slice), so without a
// reset the per-turn breakpoint would accumulate and eventually exceed the API's
// limit of 4 cache_control blocks.
func applyCacheControl(params *anthropic.BetaMessageNewParams) {
	var zero anthropic.BetaCacheControlEphemeralParam
	for i := range params.Tools {
		if params.Tools[i].OfTool != nil {
			params.Tools[i].OfTool.CacheControl = zero
		}
	}
	for i := range params.System {
		params.System[i].CacheControl = zero
	}
	for i := range params.Messages {
		for j := range params.Messages[i].Content {
			setBlockCacheControl(&params.Messages[i].Content[j], zero)
		}
	}

	cc := anthropic.NewBetaCacheControlEphemeralParam()
	// Tools are the base of the cached prefix and stable for the session. Mark the
	// last *regular* tool: server-side tools (web_search/web_fetch) are appended
	// at the end via a different union variant with no OfTool/CacheControl, so
	// marking the literal last element would be a silent no-op.
	// Nor a deferred one: the API refuses cache_control on a defer_loading
	// tool (toolreveal.go).
	for i := len(params.Tools) - 1; i >= 0; i-- {
		if params.Tools[i].OfTool != nil && !params.Tools[i].OfTool.DeferLoading.Value {
			params.Tools[i].OfTool.CacheControl = cc
			break
		}
	}
	// System prompt: large and stable across the whole session.
	if n := len(params.System); n > 0 {
		params.System[n-1].CacheControl = cc
	}
	// Rolling conversation breakpoint: the last block of the last message
	// that has blocks of its own. A tool_addition message (toolreveal.go) is
	// raw JSON with none, and when it comes last the breakpoint goes on the
	// tool results before it.
	for i := len(params.Messages) - 1; i >= 0; i-- {
		if blocks := params.Messages[i].Content; len(blocks) > 0 {
			setBlockCacheControl(&blocks[len(blocks)-1], cc)
			break
		}
	}
}

// setBlockCacheControl sets (or, with the zero value, clears) the cache_control
// on whichever variant a content block holds. Covers the block types that occur
// in a normal conversation; others are left untouched (a missing breakpoint just
// forgoes caching, never an error).
func setBlockCacheControl(block *anthropic.BetaContentBlockParamUnion, cc anthropic.BetaCacheControlEphemeralParam) {
	switch {
	case block.OfText != nil:
		block.OfText.CacheControl = cc
	case block.OfToolResult != nil:
		block.OfToolResult.CacheControl = cc
	case block.OfToolUse != nil:
		block.OfToolUse.CacheControl = cc
	case block.OfImage != nil:
		block.OfImage.CacheControl = cc
	case block.OfDocument != nil:
		block.OfDocument.CacheControl = cc
	}
}
