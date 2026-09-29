package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func asst(blocks ...anthropic.BetaContentBlockParamUnion) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: blocks}
}

func userText(s string) anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(s))
}

// toolResultMsg is the user-role message the loop uses to carry a tool_result
// back — deliberately NOT an exchange boundary.
func toolResultMsg(id string) anthropic.BetaMessageParam {
	return anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock(id, "output", false))
}

// twoToolExchanges builds:
//
//	[0] user  "q1"
//	[1] asst  text
//	[2] user  "q2"
//	[3] asst  tool_use t1
//	[4] user  tool_result t1        (not a boundary)
//	[5] asst  text
//
// so the second exchange spans a tool_use/tool_result pair, letting us prove the
// cut never lands between them.
func twoToolExchanges() []anthropic.BetaMessageParam {
	return []anthropic.BetaMessageParam{
		userText("q1"),
		asst(anthropic.NewBetaTextBlock("a1")),
		userText("q2"),
		asst(anthropic.NewBetaToolUseBlock("t1", map[string]any{}, "Bash")),
		toolResultMsg("t1"),
		asst(anthropic.NewBetaTextBlock("a2")),
	}
}

func TestRewindHistoryDefaultOne(t *testing.T) {
	h := twoToolExchanges()
	kept, ex, msgs := rewindHistory(h, 1)
	if ex != 1 {
		t.Fatalf("exchanges dropped = %d, want 1", ex)
	}
	// Second exchange starts at index 2 (user "q2"): everything from 2 on goes.
	if msgs != 4 {
		t.Fatalf("messages dropped = %d, want 4", msgs)
	}
	if len(kept) != 2 {
		t.Fatalf("kept %d messages, want 2", len(kept))
	}
	if kept[len(kept)-1].Role != anthropic.BetaMessageParamRoleAssistant {
		t.Fatalf("kept prefix should end on the first assistant turn")
	}
}

func TestRewindHistoryNeverOrphansToolPair(t *testing.T) {
	// Dropping just one exchange must not sever the t1 tool_use/tool_result pair
	// that lives inside the dropped exchange; and the surviving prefix must carry
	// no dangling tool_use and no orphan tool_result.
	h := twoToolExchanges()
	kept, _, _ := rewindHistory(h, 1)

	openUses := map[string]bool{}
	for _, m := range kept {
		for _, b := range m.Content {
			if b.OfToolUse != nil {
				openUses[b.OfToolUse.ID] = true
			}
			if b.OfToolResult != nil {
				if !openUses[b.OfToolResult.ToolUseID] {
					t.Fatalf("orphan tool_result for %s in kept history", b.OfToolResult.ToolUseID)
				}
				delete(openUses, b.OfToolResult.ToolUseID)
			}
		}
	}
	if len(openUses) != 0 {
		t.Fatalf("dangling tool_use(s) left in kept history: %v", openUses)
	}
}

func TestRewindHistoryExceedsAvailable(t *testing.T) {
	h := twoToolExchanges() // 2 exchanges
	kept, ex, msgs := rewindHistory(h, 5)
	if ex != 2 {
		t.Fatalf("exchanges dropped = %d, want 2 (all)", ex)
	}
	if msgs != len(h) {
		t.Fatalf("messages dropped = %d, want %d (all)", msgs, len(h))
	}
	if len(kept) != 0 {
		t.Fatalf("kept %d messages, want 0", len(kept))
	}
}

func TestRewindHistoryTwoExchanges(t *testing.T) {
	h := twoToolExchanges()
	kept, ex, msgs := rewindHistory(h, 2)
	if ex != 2 || msgs != 6 || len(kept) != 0 {
		t.Fatalf("rewind 2: ex=%d msgs=%d kept=%d, want 2/6/0", ex, msgs, len(kept))
	}
}

func TestRewindHistoryNonPositiveNoOp(t *testing.T) {
	h := twoToolExchanges()
	for _, n := range []int{0, -3} {
		kept, ex, msgs := rewindHistory(h, n)
		if ex != 0 || msgs != 0 || len(kept) != len(h) {
			t.Fatalf("n=%d should be a no-op, got ex=%d msgs=%d kept=%d", n, ex, msgs, len(kept))
		}
	}
}

func TestRewindHistoryNoUserPrompt(t *testing.T) {
	// A history with no genuine user prompt (only tool-result carriers) has no
	// boundary to cut on, so rewind leaves it untouched.
	h := []anthropic.BetaMessageParam{
		asst(anthropic.NewBetaToolUseBlock("t1", map[string]any{}, "Bash")),
		toolResultMsg("t1"),
	}
	kept, ex, msgs := rewindHistory(h, 1)
	if ex != 0 || msgs != 0 || len(kept) != len(h) {
		t.Fatalf("no-boundary history should be untouched, got ex=%d msgs=%d kept=%d", ex, msgs, len(kept))
	}
}

func TestRewindCommandUpdatesHistoryAndReportsDivider(t *testing.T) {
	dropped := -1
	m := &Model{
		history: twoToolExchanges(),
		sess:    &Session{Rewind: func(n int) error { dropped = n; return nil }},
	}
	m.rewindCommand(nil) // default N=1

	if len(m.history) != 2 {
		t.Fatalf("in-memory history len = %d, want 2", len(m.history))
	}
	if dropped != 4 {
		t.Fatalf("transcript asked to drop %d messages, want 4", dropped)
	}
	if !strings.Contains(m.transcript.String(), "Rewound 1 exchange") {
		t.Fatalf("scrollback missing divider, got:\n%s", m.transcript.String())
	}
}

func TestRewindCommandRejectsBadArg(t *testing.T) {
	for _, arg := range []string{"0", "-2", "abc"} {
		m := &Model{history: twoToolExchanges(), sess: &Session{}}
		before := len(m.history)
		m.rewindCommand([]string{arg})
		if len(m.history) != before {
			t.Fatalf("arg %q must not change history", arg)
		}
		if !strings.Contains(m.transcript.String(), "usage:") {
			t.Fatalf("arg %q should print a usage error, got:\n%s", arg, m.transcript.String())
		}
	}
}

func TestRewindCommandEmptyConversation(t *testing.T) {
	m := &Model{sess: &Session{}}
	m.rewindCommand(nil)
	if !strings.Contains(m.transcript.String(), "Nothing to rewind") {
		t.Fatalf("empty conversation should say nothing to rewind, got:\n%s", m.transcript.String())
	}
}
