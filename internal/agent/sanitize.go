package agent

import "github.com/anthropics/anthropic-sdk-go"

// Placeholders inserted when a transcript is structurally broken (typically
// because an earlier turn was interrupted, refused, or recorded incompletely).
// We never hide the repair — the model sees these strings and can adjust.
const (
	emptyContentPlaceholder  = "[Empty assistant turn — content was not recorded]"
	abandonedToolPlaceholder = "[Tool call abandoned — no result was recorded; treat the call as unanswered]"
)

// sanitizeMessages makes a message list satisfy the Anthropic API's structural
// rules before sending. It repairs three classes of corruption — typically
// inherited from a resumed transcript — so a /goal run (or any resume) can't be
// poisoned by an earlier broken turn:
//
//  1. Empty content: nil or [] Content gets a placeholder text block (the API
//     rejects with "messages.<i>.content: Field required" otherwise).
//  2. Orphan tool_use: every tool_use block must be answered by a tool_result
//     in the IMMEDIATELY following message. Missing pairings get a synthetic
//     tool_result with is_error=true; if the next message isn't a user one
//     (or doesn't exist), a fresh user message is inserted.
//  3. Consecutive same-role messages: the API requires user/assistant
//     alternation, so runs of the same role are merged into one message
//     (their content blocks concatenated in order).
//
// Clean conversations return the input slice unchanged. Repaired ones get an
// independent slice; original messages are never mutated.
func sanitizeMessages(messages []anthropic.BetaMessageParam) []anthropic.BetaMessageParam {
	if !needsSanitize(messages) {
		return messages
	}
	// (0) collapse consecutive same-role messages FIRST. The API requires
	// user/assistant alternation, so this has to happen regardless — but the
	// order matters for what follows. A paused server tool (stop_reason
	// pause_turn) puts its server_tool_use in one assistant message and the
	// result in the next, and only after merging are the two in the same message
	// where the pairing repair below can see them as the matched pair they are.
	// Running that repair first read the half as an orphan, dropped it, and left
	// the result unpaired — trading one 400 for its mirror image.
	out := mergeSameRole(messages)

	// (1) drop server-tool blocks that would be rejected on send. Done before
	// the empty-content repair, so a message left empty by the drop is caught
	// there rather than slipping through.
	pendingTurn := pendingAssistantIndex(out)
	for i := range out {
		if repaired, changed := repairServerToolResults(out[i].Content, i == pendingTurn); changed {
			out[i].Content = repaired
		}
		// (1b) drop web-search citations whose URL was lost to the SDK bug
		// (citations.go) in history recorded before the fix — unrecoverable at
		// send time, and the API rejects the empty url.
		if repaired, changed := repairEmptyWebSearchCitations(out[i].Content); changed {
			out[i].Content = repaired
		}
	}

	// (2) fill empty content
	for i, m := range out {
		if len(m.Content) == 0 {
			out[i].Content = []anthropic.BetaContentBlockParamUnion{
				anthropic.NewBetaTextBlock(emptyContentPlaceholder),
			}
		}
	}

	// (3) pair every client tool_use with a tool_result in the next message
	for i := 0; i < len(out); i++ {
		if out[i].Role != anthropic.BetaMessageParamRoleAssistant {
			continue
		}
		var missing []string
		for _, b := range out[i].Content {
			if tu := b.OfToolUse; tu != nil && !hasToolResultAt(out, i+1, tu.ID) {
				missing = append(missing, tu.ID)
			}
		}
		if len(missing) == 0 {
			continue
		}
		results := make([]anthropic.BetaContentBlockParamUnion, 0, len(missing))
		for _, id := range missing {
			results = append(results, anthropic.NewBetaToolResultBlock(id, abandonedToolPlaceholder, true))
		}
		if i+1 < len(out) && out[i+1].Role == anthropic.BetaMessageParamRoleUser {
			// Prepend the synthetic results so they sit adjacent to the tool_use.
			merged := make([]anthropic.BetaContentBlockParamUnion, 0, len(results)+len(out[i+1].Content))
			merged = append(merged, results...)
			merged = append(merged, out[i+1].Content...)
			out[i+1].Content = merged
		} else {
			// Insert a fresh user message at i+1 carrying the synthetic results.
			out = append(out, anthropic.BetaMessageParam{})
			copy(out[i+2:], out[i+1:])
			out[i+1] = anthropic.NewBetaUserMessage(results...)
		}
	}

	// (4) merge again: step (3) can insert a user message next to an existing
	// one. Idempotent, so it is cheap insurance rather than a second policy.
	return mergeSameRole(out)
}

// mergeSameRole collapses runs of consecutive same-role messages into one,
// concatenating their content in order. The API requires user/assistant
// alternation. The input is never mutated.
func mergeSameRole(messages []anthropic.BetaMessageParam) []anthropic.BetaMessageParam {
	merged := make([]anthropic.BetaMessageParam, 0, len(messages))
	for _, m := range messages {
		if n := len(merged); n > 0 && merged[n-1].Role == m.Role {
			combined := make([]anthropic.BetaContentBlockParamUnion, 0, len(merged[n-1].Content)+len(m.Content))
			combined = append(combined, merged[n-1].Content...)
			combined = append(combined, m.Content...)
			merged[n-1].Content = combined
			continue
		}
		merged = append(merged, m)
	}
	return merged
}

// hasToolResultAt reports whether messages[i] is a user message that carries a
// tool_result for toolUseID.
func hasToolResultAt(messages []anthropic.BetaMessageParam, i int, toolUseID string) bool {
	if i < 0 || i >= len(messages) || messages[i].Role != anthropic.BetaMessageParamRoleUser {
		return false
	}
	for _, b := range messages[i].Content {
		if tr := b.OfToolResult; tr != nil && tr.ToolUseID == toolUseID {
			return true
		}
	}
	return false
}

// needsSanitize is the fast path — when it returns false, sanitizeMessages
// returns the input unchanged with no allocation.
func needsSanitize(messages []anthropic.BetaMessageParam) bool {
	if hasBrokenServerToolResult(messages) {
		return true
	}
	if hasEmptyWebSearchCitation(messages) {
		return true
	}
	var prev anthropic.BetaMessageParamRole
	for i, m := range messages {
		if len(m.Content) == 0 {
			return true
		}
		if i > 0 && m.Role == prev {
			return true
		}
		prev = m.Role
		for _, b := range m.Content {
			if tu := b.OfToolUse; tu != nil && !hasToolResultAt(messages, i+1, tu.ID) {
				return true
			}
		}
	}
	return false
}
