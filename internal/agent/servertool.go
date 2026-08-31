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

const droppedServerToolPlaceholder = "[An earlier web search/fetch did not complete — it was interrupted, " +
	"or its result could not be restored from the transcript — and was dropped. " +
	"Re-run the search if you still need it.]"

// webServerToolNames are the server tools Klaudia enables (webtools.go). Only
// these are considered for orphan repair: another server tool (code execution,
// tool search) answers with a result block this package doesn't model, so an
// unpaired one is not evidence of corruption and is left alone.
var webServerToolNames = map[anthropic.BetaServerToolUseBlockParamName]bool{
	anthropic.BetaServerToolUseBlockParamNameWebSearch: true,
	anthropic.BetaServerToolUseBlockParamNameWebFetch:  true,
}

// orphanServerToolUseIDs returns the ids of web_search / web_fetch
// server_tool_use blocks with no result block anywhere in the same message.
//
// Unlike a client tool_use — answered by a tool_result in the NEXT (user)
// message — a server tool's result is appended to the same assistant message by
// the API. So when a turn is cancelled while a search is in flight, the
// half-built assistant message can be recorded carrying the server_tool_use
// alone. Sent back, the API rejects the whole request with
//
//	messages.N: `web_search` tool use with id `srvtoolu_…` was found without a
//	corresponding `web_search_tool_result` block
//
// and, because the bad message stays in history, every following turn fails the
// same way — a session that cannot be resumed or continued. Observed after
// interrupting a sub-agent mid-search.
func orphanServerToolUseIDs(content []anthropic.BetaContentBlockParamUnion) map[string]bool {
	var answered map[string]bool
	mark := func(id string) {
		if answered == nil {
			answered = map[string]bool{}
		}
		answered[id] = true
	}
	for _, b := range content {
		if r := b.OfWebSearchToolResult; r != nil {
			mark(r.ToolUseID)
		}
		if r := b.OfWebFetchToolResult; r != nil {
			mark(r.ToolUseID)
		}
	}
	var orphans map[string]bool
	for _, b := range content {
		su := b.OfServerToolUse
		if su == nil || !webServerToolNames[su.Name] || answered[su.ID] {
			continue
		}
		if orphans == nil {
			orphans = map[string]bool{}
		}
		orphans[su.ID] = true
	}
	return orphans
}

// repairServerToolResults replaces any web_search / web_fetch result block whose
// content did not survive round-tripping, along with the server_tool_use it
// answers, with a single text note. Returns the repaired content and whether
// anything changed. The original blocks are never mutated.
func repairServerToolResults(content []anthropic.BetaContentBlockParamUnion) ([]anthropic.BetaContentBlockParamUnion, bool) {
	drop := map[string]bool{}
	for _, b := range content {
		if id, ok := brokenResultToolUseID(b); ok {
			drop[id] = true
		}
	}
	// A server_tool_use with no result block at all is the other half of the
	// problem: the API rejects it just as hard as a corrupt result.
	for id := range orphanServerToolUseIDs(content) {
		drop[id] = true
	}
	if len(drop) == 0 {
		return content, false
	}

	out := make([]anthropic.BetaContentBlockParamUnion, 0, len(content))
	noted := false
	note := func() { // one note stands in for the whole dropped exchange
		if !noted {
			out = append(out, anthropic.NewBetaTextBlock(droppedServerToolPlaceholder))
			noted = true
		}
	}
	for _, b := range content {
		if id, ok := brokenResultToolUseID(b); ok && drop[id] {
			note()
			continue
		}
		// Drop the server_tool_use too, so nothing dangles either way.
		if su := b.OfServerToolUse; su != nil && drop[su.ID] {
			note()
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
		// An unanswered server_tool_use counts too — without this the fast path
		// short-circuits and the repair below never runs.
		if len(orphanServerToolUseIDs(m.Content)) > 0 {
			return true
		}
	}
	return false
}
