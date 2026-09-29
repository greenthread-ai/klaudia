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

// thinkingOmittedPlaceholder stands in for an assistant turn that held nothing
// but thinking, so the summary request has no message with empty content.
const thinkingOmittedPlaceholder = "[Reasoning omitted from the summary request]"

// BuildSummaryRequest returns a request that asks the model to summarize the
// given conversation. The summary instruction is appended as a final user turn.
//
// The conversation goes out without its thinking blocks. The summary request
// carries no system prompt and no tools, so it is not the conversation those
// blocks were produced in: on a model with preserved thinking (Fable 5.1, Opus
// 5.5) each block's signature records the system prompt and tool set it was
// minted under, and replaying it here fails that check — a 400 on enforced
// accounts, so compaction would fail exactly when it is needed. Removing every
// thinking block is always accepted, and a summary needs the conversation's
// text and tool calls rather than its reasoning (the API's own compaction with
// custom instructions leaves earlier thinking out on those models too). Only
// this copy is stripped; the live history is untouched.
func BuildSummaryRequest(messages []anthropic.BetaMessageParam, model anthropic.Model, maxTokens int64) anthropic.BetaMessageNewParams {
	msgs := withoutThinking(messages)
	msgs = append(msgs, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(SummaryInstruction)))
	return anthropic.BetaMessageNewParams{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  msgs,
	}
}

// withoutThinking returns a copy of messages with every thinking and
// redacted_thinking block removed. An assistant turn left with no content gets
// a short placeholder, since the API rejects an empty message. The input is
// never mutated.
func withoutThinking(messages []anthropic.BetaMessageParam) []anthropic.BetaMessageParam {
	out := make([]anthropic.BetaMessageParam, len(messages), len(messages)+1)
	copy(out, messages)
	for i, m := range messages {
		kept := make([]anthropic.BetaContentBlockParamUnion, 0, len(m.Content))
		for _, b := range m.Content {
			if b.OfThinking != nil || b.OfRedactedThinking != nil {
				continue
			}
			kept = append(kept, b)
		}
		if len(kept) == len(m.Content) {
			continue
		}
		if len(kept) == 0 {
			kept = append(kept, anthropic.NewBetaTextBlock(thinkingOmittedPlaceholder))
		}
		out[i].Content = kept
	}
	return out
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
