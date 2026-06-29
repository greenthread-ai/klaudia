package api

import (
	"strings"
	"testing"
)

func TestAssembleMessageRefusalGetsPlaceholderNotNullContent(t *testing.T) {
	// A refusal (model returned no text and no tool calls) used to marshal to
	// "content": null, which then poisoned resumed transcripts. The shim must
	// substitute a visible placeholder block instead.
	m, err := assembleMessage("openai/refuse-bot", "", nil, "stop", 0, 0)
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
	m, err := assembleMessage("any", "hello there", nil, "stop", 1, 2)
	if err != nil {
		t.Fatalf("assembleMessage: %v", err)
	}
	if len(m.Content) != 1 || m.Content[0].Text != "hello there" {
		t.Errorf("normal text should be the sole block; got %#v", m.Content)
	}
}
