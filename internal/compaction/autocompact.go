package compaction

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
)

// SummaryInstruction is appended to the conversation to elicit a structured
// summary, condensed from the JS compaction prompt (compactConversation).
const SummaryInstruction = `Your context window is nearly full. Produce a detailed summary of the conversation so far that captures everything needed to continue the work seamlessly. Include:
1. The user's overall goal and any explicit requirements or constraints.
2. Key files, functions, and decisions made, with paths.
3. What has been done so far and the current state.
4. The next steps that remain.
Write the summary as plain prose. Do not ask questions or take any further action.`

// BuildSummaryRequest returns a request that asks the model to summarize the
// given conversation. The summary instruction is appended as a final user turn.
// effort is the session's level; SummaryEffort decides what the summary call
// actually runs at.
func BuildSummaryRequest(messages []anthropic.BetaMessageParam, model anthropic.Model, maxTokens int64, effort api.Effort) anthropic.BetaMessageNewParams {
	msgs := append([]anthropic.BetaMessageParam{}, messages...)
	msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(SummaryInstruction)))
	params := anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  msgs,
	}
	if lvl := SummaryEffort(effort); lvl != api.EffortUnset {
		params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(lvl)
	}
	return params
}

// SummaryEffort is the level a compaction summary runs at, given the session's.
//
// Summarising is extraction, not reasoning: the instruction above names the
// four things to pull out of a transcript that is already in front of the
// model. Spending xhigh or max on it buys nothing and costs on every
// compaction, which on a long session is the call made most often after the
// turn itself. So it is capped at medium.
//
// A session already below medium keeps its own level — someone who asked for
// low everywhere did not ask compaction to be the exception — and an unset
// session stays unset rather than opting into effort on a provider that may
// not take it.
func SummaryEffort(session api.Effort) api.Effort {
	if session == api.EffortUnset || session == api.EffortLow {
		return session
	}
	return api.EffortMedium
}

// ReplaceWithSummary returns the post-compaction conversation: a single user
// message carrying the summary, mirroring how the JS replaces history with a
// compact-boundary + summary (here condensed to one carried-forward message).
//
// The closing line exists because the next user prompt is merged into this
// same message as an adjacent text block, and nothing marked where the
// summary stopped: a summary ending "**Goal m" followed by a prompt "alright…"
// reached the model as "**Goal malright…".
func ReplaceWithSummary(summary string) []anthropic.BetaMessageParam {
	const preamble = "[Conversation compacted to save context. Summary of the prior conversation follows.]\n\n"
	const closing = "\n\n[End of summary. The conversation continues below.]"
	return []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(preamble + strings.TrimSpace(summary) + closing)),
	}
}
