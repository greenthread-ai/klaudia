package api

import (
	"strings"
	"testing"
)

func TestAssembleMessageRefusalGetsPlaceholderNotNullContent(t *testing.T) {
	// A refusal (model returned no text and no tool calls) used to marshal to
	// "content": null, which then poisoned resumed transcripts. The shim must
	// substitute a visible placeholder block instead.
	m, err := assembleMessage("openai/refuse-bot", "", nil, "stop", 0, 0, 0)
	if err != nil {
		t.Fatalf("assembleMessage: %v", err)
	}
	if len(m.Content) == 0 {
		t.Fatalf("empty response produced empty Content — would later marshal to null")
	}
	if m.Content[0].Type != "text" {
		t.Fatalf("first block should be text; got type %q", m.Content[0].Type)
	}
	// The placeholder includes the finish reason so a human reading the transcript
	// (or the model on a later turn) sees why the response was empty.
	if !strings.Contains(m.Content[0].Text, "finish_reason=stop") {
		t.Errorf("placeholder missing the finish reason; got %q", m.Content[0].Text)
	}
}

func TestAssembleMessageNormalTextUnchanged(t *testing.T) {
	m, err := assembleMessage("any", "hello there", nil, "stop", 1, 2, 0)
	if err != nil {
		t.Fatalf("assembleMessage: %v", err)
	}
	if len(m.Content) != 1 || m.Content[0].Text != "hello there" {
		t.Errorf("normal text should be the sole block; got %#v", m.Content)
	}
}

// A reasoning model that spends its whole budget thinking streams only
// reasoning deltas, in either spelling, and ends on length. The reasoning stays
// out of the reply (and so out of history), but the placeholder says it
// happened, so the empty answer reads as a budget rather than a server fault.
func TestConsumeStreamReasoningOnlyReplyExplainsItself(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","content":""}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"The user wants "}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"a count…"}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}`,
		`data: [DONE]`,
	}, "\n")
	p := NewOpenAIProvider("https://x/v1", "k", nil, Tuning{})
	var streamed strings.Builder
	msg, _, err := p.consumeStream(strings.NewReader(sse), "openai/gpt-oss-20b", StreamSink{OnText: func(s string) { streamed.WriteString(s) }}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if streamed.Len() != 0 {
		t.Errorf("reasoning reached the text stream: %q", streamed.String())
	}
	if len(msg.Content) != 1 {
		t.Fatalf("content = %#v, want one placeholder block", msg.Content)
	}
	got := msg.Content[0].Text
	for _, want := range []string{"reasoned (23 characters, not shown)", "finish_reason=length"} {
		if !strings.Contains(got, want) {
			t.Errorf("placeholder %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "The user wants") {
		t.Errorf("reasoning text leaked into the reply: %q", got)
	}
	if msg.StopReason != "max_tokens" {
		t.Errorf("stop reason = %q, want max_tokens so the loop's truncation note fires", msg.StopReason)
	}
}
