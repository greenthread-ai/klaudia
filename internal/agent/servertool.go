package agent

import "github.com/anthropics/anthropic-sdk-go"

// A server-tool result can arrive in the history structurally broken, and the
// API rejects the whole request when it does.
//
// Two things conspire. First, until the recorder was fixed to store params, an
// assistant turn with a web_search / web_fetch result was persisted by
// marshalling the raw response, which serialised the result's content as an
// object full of leaked union field names rather than an array. Second, on
// resume that JSON is read back into a param, where the leaked content matches
// nothing and collapses to an empty error block — no results, no error code.
// Sent back, the API returns
//
//	messages.N.content.M.web_search_tool_result.content ...: Input should be a valid array
//
// and the turn dies. The recorder fix stops new transcripts being written this
// way; this repairs the ones already on disk (and any future edge) so a resume
// cannot be poisoned by one.

const droppedServerToolPlaceholder = "[An earlier web search/fetch result could not be restored from the " +
	"transcript and was dropped. Re-run the search if you still need it.]"

// repairServerToolResults replaces any web_search / web_fetch result block whose
// content did not survive round-tripping, along with the server_tool_use it
// answers, with a single text note. Returns the repaired content and whether
// anything changed. The original blocks are never mutated.
func repairServerToolResults(content []anthropic.BetaContentBlockParamUnion) ([]anthropic.BetaContentBlockParamUnion, bool) {
	brokenIDs := map[string]bool{}
	for _, b := range content {
		if id, ok := brokenResultToolUseID(b); ok {
			brokenIDs[id] = true
		}
	}
	if len(brokenIDs) == 0 {
		return content, false
	}

	out := make([]anthropic.BetaContentBlockParamUnion, 0, len(content))
	noted := false
	for _, b := range content {
		if id, ok := brokenResultToolUseID(b); ok {
			_ = id
			if !noted { // one note stands in for the whole dropped exchange
				out = append(out, anthropic.NewBetaTextBlock(droppedServerToolPlaceholder))
				noted = true
			}
			continue
		}
		// Drop the matching server_tool_use so nothing dangles.
		if su := b.OfServerToolUse; su != nil && brokenIDs[su.ID] {
			continue
		}
		out = append(out, b)
	}
	return out, true
}

// brokenResultToolUseID reports whether a block is a server-tool result with
// content that would be rejected on send, and the tool_use_id it answers.
//
// A result is sound when it carries actual results, or a genuine error with a
// code. The broken shape is the one a corrupted transcript produces: neither.
func brokenResultToolUseID(b anthropic.BetaContentBlockParamUnion) (string, bool) {
	if r := b.OfWebSearchToolResult; r != nil {
		c := r.Content
		hasResults := len(c.OfResultBlock) > 0
		hasError := c.OfError != nil && c.OfError.ErrorCode != ""
		if !hasResults && !hasError {
			return r.ToolUseID, true
		}
	}
	if r := b.OfWebFetchToolResult; r != nil {
		c := r.Content
		hasResult := c.OfRequestWebFetchResultBlock != nil
		hasError := c.OfRequestWebFetchToolResultError != nil && c.OfRequestWebFetchToolResultError.ErrorCode != ""
		if !hasResult && !hasError {
			return r.ToolUseID, true
		}
	}
	return "", false
}

// hasBrokenServerToolResult reports whether any message carries one, for the
// sanitize fast path.
func hasBrokenServerToolResult(messages []anthropic.BetaMessageParam) bool {
	for _, m := range messages {
		for _, b := range m.Content {
			if _, ok := brokenResultToolUseID(b); ok {
				return true
			}
		}
	}
	return false
}
