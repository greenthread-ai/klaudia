package hostgate

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// These tests drive the loop the way any host does — agent.New and Run, with
// a provider that asks for one tool call and then ends the turn — so what they
// check is the gate as the loop applies it, through the public API.

// testLoop is a registry waiting for a call to dispatch.
type testLoop struct{ reg *tools.Registry }

func newLoop(reg *tools.Registry) *testLoop { return &testLoop{reg: reg} }

// oneCall asks for tu once, then ends the turn.
type oneCall struct {
	tu anthropic.BetaToolUseBlock
	n  int
}

func (p *oneCall) StreamTurn(context.Context, anthropic.BetaMessageNewParams, api.StreamSink) (anthropic.BetaMessage, error) {
	p.n++
	if p.n > 1 {
		return anthropic.BetaMessage{StopReason: "end_turn"}, nil
	}
	input, _ := json.Marshal(p.tu.Input)
	raw, _ := json.Marshal(map[string]any{
		"role": "assistant", "stop_reason": "tool_use",
		"content": []any{map[string]any{"type": "tool_use", "id": p.tu.ID, "name": p.tu.Name, "input": json.RawMessage(input)}},
	})
	var m anthropic.BetaMessage
	err := json.Unmarshal(raw, &m)
	return m, err
}

// dispatch runs tu through a fresh loop over l's registry and returns its
// tool_result block.
func dispatch(t *testing.T, l *testLoop, tu anthropic.BetaToolUseBlock, opts agent.Options, emit agent.Emitter) anthropic.BetaContentBlockParamUnion {
	t.Helper()
	if opts.Prompt == "" {
		opts.Prompt = "go"
	}
	res, err := agent.New(&oneCall{tu: tu}, l.reg).Run(context.Background(), opts, emit)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := len(res.Messages) - 1; i >= 0; i-- {
		for _, b := range res.Messages[i].Content {
			if b.OfToolResult != nil && b.OfToolResult.ToolUseID == tu.ID {
				return b
			}
		}
	}
	t.Fatalf("no tool_result for %s in the run", tu.ID)
	return anthropic.BetaContentBlockParamUnion{}
}
