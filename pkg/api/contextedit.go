package api

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Server-side clearing of old tool results, in place of microcompact, for a
// conversation that holds thinking.
//
// Microcompact elides old tool results in the history Klaudia sends. On
// preserved-thinking models (Opus 5.5, Fable 5.1, Sonnet 5.5) that edits
// earlier messages, which invalidates every thinking block after them: a 400
// for accounts created on or after 2026-08-31, and with drop_block the
// model's reasoning thrown away. The API's own context editing
// (clear_tool_uses) does the same job without that cost: the check compares
// the conversation as it was sent, and the server clears its own working
// copy. (PR #63.)

// ContextManagementBeta gates context_management on a request.
const ContextManagementBeta = "context-management-2025-06-27"

// The server clears with the numbers microcompact uses (pkg/compaction, which
// this package cannot import): past 40k input tokens, keep the newest three
// tool uses, and only when at least 20k would go.
const (
	clearTrigger     = 40000
	clearKeep        = 3
	clearAtLeastToks = 20000
)

// ToolResultClearer is implemented by a provider that clears old tool results
// itself for some conversations; for those the loop leaves the history alone
// and skips microcompact.
type ToolResultClearer interface {
	ClearsToolResults(messages []anthropic.BetaMessageParam) bool
}

// ClearsToolResults reports whether requests for this conversation carry the
// server-side clearing edit: once it holds thinking, unless the endpoint has
// refused context management.
func (c *Client) ClearsToolResults(messages []anthropic.BetaMessageParam) bool {
	return !c.noClearing.Load() && historyHasThinking(messages)
}

// clearToolResults adds the clear_tool_uses edit and its beta, leaving any
// edits the caller set in place.
func clearToolResults(params *anthropic.BetaMessageNewParams) {
	edit := anthropic.BetaClearToolUses20250919EditParam{
		Trigger:      anthropic.BetaClearToolUses20250919EditTriggerUnionParam{OfInputTokens: &anthropic.BetaInputTokensTriggerParam{Value: clearTrigger}},
		Keep:         anthropic.BetaToolUsesKeepParam{Value: clearKeep},
		ClearAtLeast: anthropic.BetaInputTokensClearAtLeastParam{Value: clearAtLeastToks},
	}
	params.ContextManagement.Edits = append(params.ContextManagement.Edits,
		anthropic.BetaContextManagementConfigEditUnionParam{OfClearToolUses20250919: &edit})
	for _, b := range params.Betas {
		if string(b) == ContextManagementBeta {
			return
		}
	}
	params.Betas = append(append([]anthropic.AnthropicBeta{}, params.Betas...), ContextManagementBeta)
}

// isClearingRejected reports whether an endpoint refused context management
// itself (a proxy or partner endpoint without the beta).
func isClearingRejected(err error) bool {
	status, ok := apiStatus(err)
	if !ok || status != 400 {
		return false
	}
	d := strings.ToLower(errDetail(err))
	return strings.Contains(d, "context_management") || strings.Contains(d, ContextManagementBeta)
}
