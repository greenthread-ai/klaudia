package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/compaction"
)

// prefixRecorder answers with text and keeps summary requests apart from the
// conversation's own.
type prefixRecorder struct {
	summaries, turns []anthropic.BetaMessageNewParams
}

func (p *prefixRecorder) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	last := params.Messages[len(params.Messages)-1].Content
	if len(last) == 1 && last[0].OfText != nil && last[0].OfText.Text == compaction.SummaryInstruction {
		p.summaries = append(p.summaries, params)
	} else {
		p.turns = append(p.turns, params)
	}
	var m anthropic.BetaMessage
	raw, _ := json.Marshal(map[string]any{
		"id": "msg", "type": "message", "role": "assistant", "model": "m", "stop_reason": "end_turn",
		"content": []map[string]any{{"type": "text", "text": "a summary"}},
		"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	err := json.Unmarshal(raw, &m)
	return m, err
}

// A summary request carries the conversation's own system prompt and tools,
// so it reads the history from the prompt cache rather than paying for all of
// it at the moment it is largest: from autocompaction, and from /compact,
// which reuses the last request's. (PR #66.)
func TestSummaryRequestSharesTheConversationPrefix(t *testing.T) {
	p := &prefixRecorder{}
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	l := New(p, tools.NewRegistry(read))
	if _, err := l.Run(context.Background(), Options{Model: "claude-opus-5-5", Prompt: "hi", System: "THE SYSTEM PROMPT"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(p.turns) == 0 {
		t.Fatal("no turn was sent")
	}
	turn := p.turns[len(p.turns)-1]

	// /compact: after a turn, the summary reuses that turn's prefix.
	if _, _, err := l.Compact(context.Background(), turn.Messages, "claude-opus-5-5", 4096, api.EffortUnset); err != nil {
		t.Fatal(err)
	}
	// Autocompaction, forced: the summary uses this run's prefix.
	var calib compaction.Calibration
	l.compact(context.Background(), turn.Messages, Options{Model: "claude-opus-5-5"}, 4096, nil, &calib,
		requestPrefix{system: turn.System, tools: turn.Tools, betas: turn.Betas}, true)

	if len(p.summaries) != 2 {
		t.Fatalf("%d summary requests, want 2", len(p.summaries))
	}
	for i, s := range p.summaries {
		if len(s.System) == 0 || s.System[0].Text != turn.System[0].Text {
			t.Errorf("summary %d: system prompt not the conversation's", i)
		}
		if len(s.Tools) != len(turn.Tools) || len(s.Tools) == 0 {
			t.Errorf("summary %d: %d tools, want the conversation's %d", i, len(s.Tools), len(turn.Tools))
		}
	}
}
