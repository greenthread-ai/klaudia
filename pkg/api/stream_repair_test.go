package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// TestRepairFixesActualSDKMarshalFailure pins our workaround to the real bug:
// json.Marshal on a content block with empty non-nil Input fails with the exact
// "unexpected end of JSON input" error the user saw. After repair it succeeds.
// If a future SDK version stops crashing on this — e.g. by normalising empty
// RawMessage internally — this test will reveal it (the "before" branch will
// stop failing) and we can remove repairInvalidToolInputs.
func TestRepairFixesActualSDKMarshalFailure(t *testing.T) {
	// Pin the workaround to the three real shapes that fail upstream — if any
	// of these stops failing in a future SDK / stdlib, the "before" branch
	// will break and we'll know we can shrink or remove the repair.
	shapes := map[string][]byte{
		"empty non-nil":   []byte(""),
		"truncated":       []byte(`{"argument":`),
		"unclosed string": []byte(`{"x": "abc`),
	}
	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			block := anthropic.BetaContentBlockUnion{Type: "tool_use", Input: shape}

			if _, err := json.Marshal(block); err == nil {
				t.Fatal("expected json.Marshal to fail; SDK/stdlib may have changed — consider removing repairInvalidToolInputs")
			} else if !strings.Contains(err.Error(), "unexpected end of JSON input") {
				t.Fatalf("error shape changed; got %v", err)
			}

			acc := anthropic.BetaMessage{Content: []anthropic.BetaContentBlockUnion{block}}
			repairInvalidToolInputs(&acc, anthropic.BetaRawMessageStreamEventUnion{Type: "message_stop"})
			if _, err := json.Marshal(acc.Content[0]); err != nil {
				t.Fatalf("marshal after repair must succeed; got %v", err)
			}
		})
	}
}

// TestRepairEmptyToolInputsOnMessageStop replicates the SDK bug we work around:
// at the message_stop event, the SDK does json.Marshal(acc) to refresh JSON.raw.
// If any content block's Input is a zero-length json.RawMessage (observed when
// a tool_use start event arrived with Input: null and no input_json_delta
// followed), the marshal returns "unexpected end of JSON input" and the whole
// stream aborts mid-iteration. The repair must patch any such Input to "{}"
// BEFORE the marshal happens so the rest of the message is preserved.
func TestRepairInvalidToolInputsOnMessageStop(t *testing.T) {
	// Three invalid shapes all produce the same "unexpected end of JSON input"
	// error from the encoder; all three need patching. Nil and valid are left
	// alone.
	acc := anthropic.BetaMessage{
		Content: []anthropic.BetaContentBlockUnion{
			{Type: "tool_use", Input: []byte("")},             // empty non-nil
			{Type: "tool_use", Input: []byte(`{"argument":`)}, // truncated mid-key (max_tokens cutoff)
			{Type: "tool_use", Input: []byte(`{"x": "abc`)},   // unclosed string
			{Type: "tool_use", Input: []byte(`{"x": 1}`)},     // valid — must NOT be touched
			{Type: "tool_use", Input: nil},                    // nil — must NOT be touched (marshals as null)
			{Type: "text"},                                    // non-tool block, nil Input — must NOT be touched
		},
	}
	stopEv := anthropic.BetaRawMessageStreamEventUnion{Type: "message_stop"}

	repairInvalidToolInputs(&acc, stopEv)

	want := []struct {
		idx  int
		want string
	}{
		{0, "{}"},
		{1, "{}"},
		{2, "{}"},
		{3, `{"x": 1}`},
	}
	for _, tc := range want {
		if got := string(acc.Content[tc.idx].Input); got != tc.want {
			t.Errorf("Content[%d].Input = %q, want %q", tc.idx, got, tc.want)
		}
	}
	if acc.Content[4].Input != nil {
		t.Errorf("nil Input must stay nil (marshals as null); got %q", acc.Content[4].Input)
	}
	if acc.Content[5].Input != nil {
		t.Errorf("non-tool block nil Input must stay nil; got %q", acc.Content[5].Input)
	}
}

func TestRepairEmptyToolInputsOnContentBlockStop(t *testing.T) {
	// content_block_stop only marshals the LAST block, so only that one needs
	// repair. Earlier blocks are untouched because their content_block_stop
	// already passed.
	acc := anthropic.BetaMessage{
		Content: []anthropic.BetaContentBlockUnion{
			{Type: "tool_use", Input: []byte("")}, // earlier block — leave as-is
			{Type: "tool_use", Input: []byte("")}, // the block this stop event applies to
		},
	}
	stopEv := anthropic.BetaRawMessageStreamEventUnion{Type: "content_block_stop"}

	repairInvalidToolInputs(&acc, stopEv)

	if string(acc.Content[0].Input) != "" {
		t.Errorf("earlier block must not be touched by content_block_stop; got %q", acc.Content[0].Input)
	}
	if string(acc.Content[1].Input) != "{}" {
		t.Errorf("last block must be patched; got %q", acc.Content[1].Input)
	}
}

func TestRepairEmptyToolInputsNoOpOnOtherEvents(t *testing.T) {
	// Events that don't trigger json.Marshal in the SDK must NOT be repaired —
	// otherwise we'd interfere with the SDK's own input-delta accumulation
	// that legitimately mutates the Input field over multiple events.
	for _, eventType := range []string{
		"message_start", "message_delta", "content_block_start", "content_block_delta",
	} {
		acc := anthropic.BetaMessage{
			Content: []anthropic.BetaContentBlockUnion{{Type: "tool_use", Input: []byte("")}},
		}
		ev := anthropic.BetaRawMessageStreamEventUnion{Type: eventType}
		repairInvalidToolInputs(&acc, ev)
		if string(acc.Content[0].Input) != "" {
			t.Errorf("event %q must not trigger repair; Input was modified to %q", eventType, acc.Content[0].Input)
		}
	}
}

func TestRepairEmptyToolInputsNoOpOnEmptyContent(t *testing.T) {
	// The repair must be safe to call even when acc.Content is empty — happens
	// in early stream events before the first content_block_start.
	acc := anthropic.BetaMessage{}
	for _, eventType := range []string{"message_stop", "content_block_stop"} {
		ev := anthropic.BetaRawMessageStreamEventUnion{Type: eventType}
		repairInvalidToolInputs(&acc, ev) // must not panic
	}
}
