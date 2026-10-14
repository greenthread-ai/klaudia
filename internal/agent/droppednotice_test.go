package agent

import (
	"context"
	"github.com/greenthread-ai/klaudia/internal/tools"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func notices(msgs []anthropic.BetaMessageParam, reported map[string]bool) []string {
	return noticesResumed(msgs, reported, nil)
}

func noticesResumed(msgs []anthropic.BetaMessageParam, reported, resumed map[string]bool) []string {
	var out []string
	announceDroppedServerTools(msgs, func(ev Event) {
		if ev.Type == "notice" {
			out = append(out, ev.Content)
		}
	}, reported, resumed)
	return out
}

// The repair was correct and silent, and the silence was the bug. A search
// that never completed is replaced with a placeholder the model reads on the
// next request, so the model recovers — but the user saw a search in the
// transcript with no results, no error, and no way to know the answer that
// followed had been reasoned without it.
func TestDroppedWebSearchIsAnnounced(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("what's the news?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock("Let me search."),
			webSearchUse("srvtoolu_1"), // never answered
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("never mind, do this instead")),
	}

	got := notices(msgs, map[string]bool{})
	if len(got) != 1 {
		t.Fatalf("got %d notices, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "web search") {
		t.Errorf("the notice should name what was lost: %q", got[0])
	}
	if !strings.Contains(got[0], "re-run") {
		t.Errorf("the notice should say what to do about it: %q", got[0])
	}
}

// The same exchange is still broken on the next turn, and the one after that.
// Announcing it every time would turn one lost search into a permanent banner
// for the rest of a resumed session.
func TestADroppedSearchIsAnnouncedOnce(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse("srvtoolu_1"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("something else")),
	}

	reported := map[string]bool{}
	if got := notices(msgs, reported); len(got) != 1 {
		t.Fatalf("first pass: got %d notices, want 1", len(got))
	}
	if got := notices(msgs, reported); len(got) != 0 {
		t.Errorf("second pass announced it again: %v", got)
	}
}

// A server tool the API withheld because the same batch held client tools is
// waiting on purpose — it runs on the next request once the client results go
// back. Announcing that would be a false alarm about work that is about to
// happen, and the user cannot tell a false alarm from a real one.
func TestAPendingSearchIsNotAnnounced(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("check the docs and the repo")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse("srvtoolu_1"),
			anthropic.NewBetaToolUseBlock("toolu_1", map[string]any{"pattern": "*.go"}, "Glob"),
		}},
		// Only tool_results: the turn is still being answered.
		anthropic.NewBetaUserMessage(anthropic.NewBetaToolResultBlock("toolu_1", "a.go", false)),
	}

	if got := notices(msgs, map[string]bool{}); len(got) != 0 {
		t.Errorf("a pending search was announced as dropped: %v", got)
	}
}

// A paused search splits its call and result across two assistant messages, so
// before merging it looks unpaired when it is not. The announcement has to
// merge for the same reason sanitizeMessages does.
func TestAPausedSearchIsNotAnnounced(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("search please")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse("srvtoolu_1"),
		}},
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchResult("srvtoolu_1"),
			anthropic.NewBetaTextBlock("here is what I found"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("thanks")),
	}

	if got := notices(msgs, map[string]bool{}); len(got) != 0 {
		t.Errorf("a paused-then-resumed search was announced as dropped: %v", got)
	}
}

// A result whose content did not survive round-tripping is the other shape the
// repair handles, and it is just as invisible: the model is handed a search
// that returned nothing at all.
func TestAResultLostToTheTranscriptIsAnnounced(t *testing.T) {
	broken := anthropic.BetaContentBlockParamUnion{
		OfWebSearchToolResult: &anthropic.BetaWebSearchToolResultBlockParam{
			ToolUseID: "srvtoolu_1",
			// Neither results nor an error code: what a corrupted transcript
			// reads back as.
			Content: anthropic.BetaWebSearchToolResultBlockParamContentUnion{},
		},
	}
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse("srvtoolu_1"), broken,
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("and now this")),
	}

	got := notices(msgs, map[string]bool{})
	if len(got) != 1 {
		t.Fatalf("got %d notices, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "web search") {
		t.Errorf("the notice should name what was lost: %q", got[0])
	}
}

// A clean conversation must stay quiet.
func TestNoNoticeForAHealthyConversation(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("search please")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse("srvtoolu_1"), webSearchResult("srvtoolu_1"),
			anthropic.NewBetaTextBlock("found it"),
		}},
	}

	if got := notices(msgs, map[string]bool{}); len(got) != 0 {
		t.Errorf("announced a drop in a healthy conversation: %v", got)
	}
}

// A fetch is not a search, and a user chasing down what went missing needs to
// know which.
func TestTheNoticeDistinguishesFetchFromSearch(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("read this page")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaServerToolUseBlock("srvtoolu_1", map[string]any{"url": "https://example.com"},
				anthropic.BetaServerToolUseBlockParamNameWebFetch),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("never mind")),
	}

	got := notices(msgs, map[string]bool{})
	if len(got) != 1 {
		t.Fatalf("got %d notices, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "web fetch") {
		t.Errorf("notice = %q, want it to name a web fetch", got[0])
	}
}

// The notice used to blame the transcript for every drop, including the
// ordinary case of a search still running when the user interrupted the turn.
// Reading "could not be restored from the transcript" for that sent someone
// looking for a corruption bug that had already been fixed.
func TestDroppedNoticeNamesTheRightCause(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("what's the news?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse("srvtoolu_live"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("never mind")),
	}

	got := noticesResumed(msgs, map[string]bool{}, nil)
	if len(got) != 1 || !strings.Contains(got[0], "interrupted before it returned") {
		t.Fatalf("an in-session drop should say it was interrupted: %q", got)
	}

	// Same shape, but this one arrived broken from disk.
	got = noticesResumed(msgs, map[string]bool{}, map[string]bool{"srvtoolu_live": true})
	if len(got) != 1 || !strings.Contains(got[0], "saved transcript") {
		t.Fatalf("a resumed drop should blame the transcript: %q", got)
	}
}

// resumedUnpairedIDs is what separates the two, so it must see the same
// orphans the announcement does.
func TestResumedUnpairedIDsFindsOrphans(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("q")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			webSearchUse("srvtoolu_orphan"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("next")),
	}
	got := resumedUnpairedIDs(msgs)
	if !got["srvtoolu_orphan"] {
		t.Errorf("orphan not found: %v", got)
	}
}

// Reported from a live session: the same three notices printed above every
// reply, turn after turn. The repair was applied to the copy sent to the API
// while the conversation kept its orphans, so each turn rediscovered them — and
// since a TUI turn is one Run, the per-Run "already reported" set never helped.
func TestDroppedServerToolsAreRepairedOnce(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("what's new?")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaTextBlock("Looking."),
			webSearchUse("srvtoolu_lost"),
		}},
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("carry on")),
	}

	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		{StopReason: "end_turn", Content: []anthropic.BetaContentBlockUnion{{Type: "text", Text: "ok"}}},
		{StopReason: "end_turn", Content: []anthropic.BetaContentBlockUnion{{Type: "text", Text: "ok"}}},
	}}
	loop := New(provider, tools.NewRegistry())

	count := func(res Result, err error) (int, Result) {
		t.Helper()
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		return 0, res
	}

	var notices int
	emit := func(e Event) {
		if e.Type == "notice" && strings.Contains(e.Content, "dropped from the conversation") {
			notices++
		}
	}

	_, res := count(loop.Run(context.Background(), Options{Prompt: "turn one", InitialMessages: history}, emit))
	if notices != 1 {
		t.Fatalf("first turn emitted %d notices, want 1", notices)
	}
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if su := b.OfServerToolUse; su != nil && su.ID == "srvtoolu_lost" {
				t.Fatal("the orphan survived in the returned conversation, so it will be found again next turn")
			}
		}
	}

	// The next turn resumes from what the last one returned, exactly as the TUI
	// does. Nothing should be announced again.
	notices = 0
	_, _ = count(loop.Run(context.Background(), Options{Prompt: "turn two", InitialMessages: res.Messages}, emit))
	if notices != 0 {
		t.Errorf("turn two re-announced %d dropped searches", notices)
	}
}
