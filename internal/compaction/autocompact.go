package compaction

import "github.com/anthropics/anthropic-sdk-go"

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
func BuildSummaryRequest(messages []anthropic.BetaMessageParam, model anthropic.Model, maxTokens int64) anthropic.BetaMessageNewParams {
	msgs := append([]anthropic.BetaMessageParam{}, messages...)
	msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(SummaryInstruction)))
	return anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  msgs,
	}
}

// ReplaceWithSummary returns the post-compaction conversation: a single user
// message carrying the summary, mirroring how the JS replaces history with a
// compact-boundary + summary (here condensed to one carried-forward message).
func ReplaceWithSummary(summary string) []anthropic.BetaMessageParam {
	const preamble = "[Conversation compacted to save context. Summary of the prior conversation follows.]\n\n"
	return []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(preamble + summary)),
	}
}
