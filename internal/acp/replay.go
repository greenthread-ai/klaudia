package acp

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
)

// Replaying a loaded conversation.
//
// session/load hands the client a thread it has never seen, so the agent has to
// re-tell it: the spec requires the whole conversation to go out as
// session/update notifications before the request returns. The client has the
// session id already (it supplied it), so unlike session/new there is no
// ordering problem — these can be sent inline.
//
// The replay is deliberately in wire order rather than summarised. Tool calls go
// out as the live stream sends them — a tool_call when the assistant asked for
// it, a tool_call_update when its result comes back in the next user message —
// so a client needs no separate code path for a loaded session.

// replay re-sends history as notifications. It returns the number of messages
// that produced at least one update, which the caller logs: a transcript that
// replays to nothing is the signature of a format change, and it is otherwise
// indistinguishable from an empty session.
func (t *translator) replay(history []anthropic.BetaMessageParam) int {
	shown := 0
	for _, m := range history {
		if t.replayMessage(m) {
			shown++
		}
	}
	return shown
}

func (t *translator) replayMessage(m anthropic.BetaMessageParam) bool {
	any := false
	for _, b := range m.Content {
		switch {
		case b.OfText != nil:
			text := b.OfText.Text
			if _, marker := api.RevealedName(b); marker || strings.TrimSpace(text) == "" {
				continue
			}
			if m.Role == anthropic.BetaMessageParamRoleAssistant {
				t.post(agentMessage(text))
			} else {
				t.post(userMessage(text))
			}
			any = true

		case b.OfToolUse != nil:
			tu := b.OfToolUse
			// A TodoWrite in the history is a plan in the history. Suppressing
			// the id here matters as much as it does live: the tool_result
			// further down this same replay would otherwise patch a call the
			// client was never sent.
			if entries, ok := planEntries(tu.Name, tu.Input); ok {
				t.suppress(tu.ID)
				t.post(planUpdate{Kind: "plan", Entries: entries})
				any = true
				continue
			}
			// No diff content, unlike the live path. A diff needs the file's
			// contents from before the edit, and the only moment those exist is
			// just before the tool runs. Reading the file now and calling it
			// "before" would show the user a diff of something that never
			// happened.
			t.post(toolCallStart{
				Kind:       "tool_call",
				ToolCallID: tu.ID,
				Name:       tu.Name,
				Title:      toolTitle(tu.Name, tu.Input),
				ToolKind:   toolKind(tu.Name),
				Status:     statusPending,
				Locations:  toolLocations(tu.Name, tu.Input),
				RawInput:   tu.Input,
			})
			any = true

		case b.OfToolResult != nil:
			tr := b.OfToolResult
			if t.isSuppressed(tr.ToolUseID) {
				continue
			}
			status := statusCompleted
			if tr.IsError.Value {
				status = statusFailed
			}
			patch := toolCallPatch{
				Kind:       "tool_call_update",
				ToolCallID: tr.ToolUseID,
				Status:     status,
			}
			if text := toolResultText(tr); text != "" {
				patch.Content = []toolCallContent{toolText(text)}
			}
			t.post(patch)
			any = true
		}
	}
	// A tool_use with no tool_result stays pending, and that is accurate: the
	// turn was interrupted and the call never finished. Marking it failed would
	// claim an error the transcript does not record.
	return any
}

// toolResultText flattens a recorded tool result. Image blocks are dropped
// rather than re-sent: a replay of twenty screenshots is a slow, enormous
// session/update storm for a conversation the user is only reopening.
func toolResultText(tr *anthropic.BetaToolResultBlockParam) string {
	var b strings.Builder
	for _, c := range tr.Content {
		if c.OfText == nil {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(c.OfText.Text)
	}
	return b.String()
}
