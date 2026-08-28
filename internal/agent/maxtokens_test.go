package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// maxTokensToolUse builds an assistant turn that stopped at the output limit
// mid-tool-call: the arguments never closed, so the stream layer left the input
// as "{}" (api.repairInvalidToolInputs). This is the exact shape the loop must
// refuse to dispatch.
func maxTokensToolUse(t *testing.T, id, name string) anthropic.BetaMessage {
	t.Helper()
	raw := fmt.Sprintf(`{"role":"assistant","stop_reason":"max_tokens",
		"content":[{"type":"tool_use","id":%q,"name":%q,"input":{}}]}`, id, name)
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("unmarshal truncated turn: %v", err)
	}
	return m
}

// The reported bug: a large Write hit the output-token limit mid-arguments, was
// patched to empty "{}" input, then dispatched — producing a cryptic "missing
// file_path, content" error and a retry loop. The loop must instead recognise
// the truncation, skip the call, and return an actionable note.
func TestTruncatedToolUseIsNotDispatched(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "findings.md")

	write, err := tools.NewWrite()
	if err != nil {
		t.Fatal(err)
	}
	reg := tools.NewRegistry(write)

	provider := &scriptedProvider{turns: []anthropic.BetaMessage{
		maxTokensToolUse(t, "tu_write", "Write"),
		// next turn: scriptedProvider returns end_turn automatically
	}}

	loop := New(provider, reg)
	res, err := loop.Run(context.Background(), Options{
		Model:      "test",
		Permission: bypassPerm(),
		MaxTurns:   5,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("a truncated Write must not create the file (err=%v)", err)
	}
	if !messagesContain(res.Messages, "output-token limit") {
		t.Errorf("tool_result did not carry the truncation note")
	}
	// It must NOT surface the raw schema error the user saw.
	if messagesContain(res.Messages, "missing properties") || messagesContain(res.Messages, "file_path, content") {
		t.Errorf("the cryptic schema error leaked instead of the truncation note")
	}
}

// A complete tool_use earlier in a max_tokens turn is real and must still run;
// only the truncated trailing block is skipped.
func TestTruncatedGuardSparesCompleteSiblingToolUse(t *testing.T) {
	// stop_reason max_tokens, but the (only) tool_use has real input and is the
	// final block, so it is NOT empty — it must be treated as complete.
	inB, _ := json.Marshal(map[string]any{"file_path": "/x", "content": "y"})
	raw := fmt.Sprintf(`{"role":"assistant","stop_reason":"max_tokens",
		"content":[{"type":"tool_use","id":"tu1","name":"Write","input":%s}]}`, inB)
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := truncatedToolUseID(m); ok {
		t.Error("a tool_use with complete arguments was wrongly flagged as truncated")
	}
}

func TestIsEmptyToolInput(t *testing.T) {
	for _, in := range []string{"", "{}", " {} ", "null"} {
		if !isEmptyToolInput([]byte(in)) {
			t.Errorf("%q should be empty", in)
		}
	}
	if isEmptyToolInput([]byte(`{"file_path":"/x"}`)) {
		t.Error("non-empty input flagged as empty")
	}
}
