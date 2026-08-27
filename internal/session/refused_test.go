package session

import (
	"encoding/json"
	"testing"
)

func asstEntry(t *testing.T, msg string) Entry {
	t.Helper()
	return Entry{Type: "assistant", Message: json.RawMessage(msg)}
}
func userEntry(msg string) Entry {
	return Entry{Type: "user", Message: json.RawMessage(msg)}
}

// The signature that matters going forward: a refusal is persisted as an
// assistant turn with no content.
func TestLastTurnRefusedOnEmptyContent(t *testing.T) {
	entries := []Entry{
		userEntry(`{"role":"user","content":[{"type":"text","text":"hi"}]}`),
		asstEntry(t, `{"role":"assistant","content":[]}`),
	}
	if !LastTurnRefused(entries) {
		t.Error("an empty final assistant turn was not detected as a refusal")
	}
}

// Older transcripts still carry stop_reason; use it when present.
func TestLastTurnRefusedOnStopReason(t *testing.T) {
	entries := []Entry{
		asstEntry(t, `{"role":"assistant","stop_reason":"refusal","content":[{"type":"text","text":"I can't help with that."}]}`),
	}
	if !LastTurnRefused(entries) {
		t.Error("stop_reason refusal was not detected")
	}
}

// A normal answer is not a refusal, and must be resumed.
func TestNormalTurnIsNotRefused(t *testing.T) {
	for _, msg := range []string{
		`{"role":"assistant","content":[{"type":"text","text":"here is the answer"}]}`,
		`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`,
		`{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"Bash","input":{}}]}`,
	} {
		if LastTurnRefused([]Entry{asstEntry(t, msg)}) {
			t.Errorf("a normal turn was mistaken for a refusal: %s", msg)
		}
	}
}

// Only the LAST assistant turn counts — an earlier refusal that was recovered
// from must not condemn the session.
func TestOnlyTheFinalAssistantTurnCounts(t *testing.T) {
	entries := []Entry{
		asstEntry(t, `{"role":"assistant","content":[]}`), // refused earlier
		userEntry(`{"role":"user","content":[{"type":"text","text":"try again differently"}]}`),
		asstEntry(t, `{"role":"assistant","content":[{"type":"text","text":"sure, here you go"}]}`),
	}
	if LastTurnRefused(entries) {
		t.Error("a recovered session was treated as refused because of an earlier refusal")
	}
}

// A trailing user message (e.g. queued but unanswered) does not change the
// verdict — we look back to the last assistant turn.
func TestTrailingUserMessageIsSkipped(t *testing.T) {
	entries := []Entry{
		asstEntry(t, `{"role":"assistant","content":[]}`),
		userEntry(`{"role":"user","content":[{"type":"text","text":"hello?"}]}`),
	}
	if !LastTurnRefused(entries) {
		t.Error("a refusal followed by an unanswered user message was missed")
	}
}

// A whitespace-only text block is as empty as no block.
func TestWhitespaceOnlyIsRefused(t *testing.T) {
	if !LastTurnRefused([]Entry{asstEntry(t, `{"role":"assistant","content":[{"type":"text","text":"  \n"}]}`)}) {
		t.Error("a whitespace-only turn was not treated as empty")
	}
}

// Unreadable JSON must not be guessed as a refusal — better to resume than to
// silently discard a session over a parse error.
func TestUnreadableEntryIsNotRefused(t *testing.T) {
	if LastTurnRefused([]Entry{asstEntry(t, `{not json`)}) {
		t.Error("an unreadable entry was treated as a refusal")
	}
}

// No assistant turns at all (brand-new or user-only) is not a refusal.
func TestNoAssistantTurns(t *testing.T) {
	if LastTurnRefused(nil) || LastTurnRefused([]Entry{userEntry(`{"role":"user","content":[]}`)}) {
		t.Error("absence of an assistant turn was treated as a refusal")
	}
}
