package agent

import (
	"strings"
	"testing"
)

// A normal turn says nothing extra.
func TestTurnNoteSilentOnNormalEnds(t *testing.T) {
	for _, sr := range []string{"end_turn", "tool_use", "stop_sequence", "user_halt", "", "max_turns"} {
		if note := TurnNote(sr, true); note != "" {
			t.Errorf("TurnNote(%q) = %q, want silent", sr, note)
		}
	}
}

// The case that prompted this: a refusal must be named, and must point at the
// way out, because in a resumed session it recurs on unrelated prompts.
func TestRefusalIsExplainedAndActionable(t *testing.T) {
	note := TurnNote("refusal", false)
	if note == "" {
		t.Fatal("a refusal produced no note — the user would see dead air")
	}
	if !strings.Contains(note, "declined") {
		t.Errorf("note does not say what happened: %q", note)
	}
	// The compounding factor is the whole reason it needs an escape hatch.
	if !strings.Contains(note, "/clear") && !strings.Contains(note, "new-session") {
		t.Errorf("note does not offer a way out of a refusal loop: %q", note)
	}
	if !strings.Contains(note, "earlier context") {
		t.Errorf("note does not explain why an unrelated prompt keeps refusing: %q", note)
	}
}

// Truncation depends on whether anything was produced first.
func TestMaxTokensNoteDependsOnPartialText(t *testing.T) {
	withText := TurnNote("max_tokens", true)
	if !strings.Contains(withText, "cut off") || !strings.Contains(withText, "continue") {
		t.Errorf("truncation-with-text note = %q", withText)
	}
	noText := TurnNote("max_tokens", false)
	if !strings.Contains(noText, "before saying anything") {
		t.Errorf("truncation-without-text note = %q", noText)
	}
	if withText == noText {
		t.Error("the note should differ by whether a partial answer exists")
	}
}

func TestContextExceededPointsAtCompaction(t *testing.T) {
	note := TurnNote("model_context_window_exceeded", false)
	if !strings.Contains(note, "/compact") && !strings.Contains(note, "/clear") {
		t.Errorf("context-exceeded note offers no remedy: %q", note)
	}
}

func TestTurnEndedEmpty(t *testing.T) {
	for _, s := range []string{"", "   ", "\n\t "} {
		if !TurnEndedEmpty(s) {
			t.Errorf("TurnEndedEmpty(%q) = false, want true", s)
		}
	}
	if TurnEndedEmpty("an answer") {
		t.Error("real text was reported as empty")
	}
}
