package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
)

type oneShot struct {
	got  anthropic.BetaMessageNewParams
	stop string
}

func (p *oneShot) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.got = params
	var m anthropic.BetaMessage
	err := json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"`+p.stop+`","content":[{"type":"text","text":"fix: a thing"}]}`), &m)
	return m, err
}

// Complete is one question with no tools and no history, and a reply cut off
// at the cap is refused rather than taken as the answer.
func TestCompleteAsksOneQuestion(t *testing.T) {
	p := &oneShot{stop: "end_turn"}
	l := New(p, tools.NewRegistry())
	got, err := l.Complete(context.Background(), "draft it", "m", 200, api.EffortMax)
	if err != nil || got != "fix: a thing" {
		t.Fatalf("Complete = %q, %v", got, err)
	}
	if len(p.got.Messages) != 1 || len(p.got.Tools) != 0 || p.got.MaxTokens != 200 {
		t.Errorf("request = %d messages, %d tools, max %d", len(p.got.Messages), len(p.got.Tools), p.got.MaxTokens)
	}
	if p.got.OutputConfig.Effort != "medium" {
		t.Errorf("effort = %q, want capped at medium", p.got.OutputConfig.Effort)
	}
	p.stop = "max_tokens"
	if _, err := l.Complete(context.Background(), "x", "m", 200, api.EffortUnset); err == nil {
		t.Error("a truncated reply was accepted")
	}
}
