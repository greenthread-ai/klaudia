package agent

import (
	"context"
	"encoding/json"
	"strings"
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

// The inbox drains once, and a background report is framed as news, not as
// the user interrupting with an order.
func TestInboxAndReportFraming(t *testing.T) {
	var b Inbox
	b.Add("<background-job id=\"bash_1\">done</background-job>")
	b.Add("<background-job id=\"bash_2\">done</background-job>")
	in := b.Interject()
	if !strings.Contains(in.Text, "bash_1") || !strings.Contains(in.Text, "bash_2") {
		t.Fatalf("drained %q", in.Text)
	}
	if !b.Interject().Empty() {
		t.Error("the inbox did not empty")
	}
	msg, ok := steerMessage(in)
	if !ok {
		t.Fatal("no message")
	}
	b2, _ := json.Marshal(msg)
	if strings.Contains(string(b2), "The user interrupted") || !strings.Contains(string(b2), "Background work you started has reported back") {
		t.Errorf("framing = %s", b2)
	}
}

// A background task's LastAt starts at Started and moves with each progress
// line, so a frontend can say how long it has been quiet.
func TestBackgroundTaskTracksLastProgress(t *testing.T) {
	b := NewBackground(t.Context(), nil)
	step := make(chan struct{})
	done := make(chan struct{})
	b.Start("x", func(_ context.Context, progress func(string)) (string, error) {
		<-step
		progress("step")
		close(done)
		<-step
		return "", nil
	})
	first := b.List()[0]
	if !first.LastAt.Equal(first.Started) {
		t.Errorf("LastAt = %v, want Started %v", first.LastAt, first.Started)
	}
	step <- struct{}{}
	<-done
	if got := b.List()[0]; !got.LastAt.After(first.Started) || got.Last != "step" {
		t.Errorf("after progress: %+v", got)
	}
	step <- struct{}{}
}
