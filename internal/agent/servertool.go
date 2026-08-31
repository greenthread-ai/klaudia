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

// serverToolResultID returns the tool_use_id a web_search / web_fetch result
// block answers, and whether the block is one.
func serverToolResultID(b anthropic.BetaContentBlockParamUnion) (string, bool) {
	if r := b.OfWebSearchToolResult; r != nil {
		return r.ToolUseID, true
	}
	if r := b.OfWebFetchToolResult; r != nil {
		return r.ToolUseID, true
	}
	return "", false
}

// unpairedServerToolIDs returns the tool_use_ids whose server-tool blocks do not
// form a valid pair within one message, in either direction.
//
// Unlike a client tool_use — answered by a tool_result in the NEXT (user)
// message — a server tool's result is appended to the same assistant message by
// the API, and it must come *after* its server_tool_use. Both halves can go
// missing, and the API has a distinct 400 for each:
//
//	`web_search` tool use with id `srvtoolu_…` was found without a
//	corresponding `web_search_tool_result` block
//
//	unexpected `tool_use_id` found in `web_search_tool_result` blocks:
//	srvtoolu_…. Each `web_search_tool_result` block must have a corresponding
//	`server_tool_use` block before it
//
// Either way the bad message stays in history and every following turn fails the
// same way, so the session can be neither continued nor resumed. The first shape
// comes from a turn cancelled with a search in flight; the second from a result
// whose call was lost.
//
// This runs on merged messages (sanitize.go collapses same-role runs first):
// a paused search splits its call and result across two assistant messages, and
// only after merging are they visibly the pair they are.
func unpairedServerToolIDs(content []anthropic.BetaContentBlockParamUnion) map[string]bool {
	answered := map[string]bool{}
	for _, b := range content {
		if id, ok := serverToolResultID(b); ok {
			answered[id] = true
		}
	}
	var unpaired map[string]bool
	drop := func(id string) {
		if unpaired == nil {
			unpaired = map[string]bool{}
		}
		unpaired[id] = true
	}
	called := map[string]bool{}
	for _, b := range content {
		if su := b.OfServerToolUse; su != nil && webServerToolNames[su.Name] {
			called[su.ID] = true
			if !answered[su.ID] { // a call whose result never arrived
				drop(su.ID)
			}
			continue
		}
		// A result must be preceded by its call, so `called` is consulted as it
		// stands here rather than over the whole message.
		if id, ok := serverToolResultID(b); ok && !called[id] {
			drop(id)
		}
	}
	return unpaired
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
	// A server-tool block with no partner is the other half of the problem: the
	// API rejects it just as hard as a corrupt result, in either direction.
	for id := range unpairedServerToolIDs(content) {
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
		// Any result block for a doomed id, not just a corrupt one: a perfectly
		// well-formed result whose call is missing is still rejected on send.
		if id, ok := serverToolResultID(b); ok && drop[id] {
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
		// An unpaired server-tool block counts too — without this the fast path
		// short-circuits and the repair below never runs.
		if len(unpairedServerToolIDs(m.Content)) > 0 {
			return true
		}
	}
	return false
}
