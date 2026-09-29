package agent

import (
	"context"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// noticeProvider says something about how it served the turn — what the
// fallback wrapper does when it moves a request to the fallback model.
type noticeProvider struct{}

func (noticeProvider) StreamTurn(_ context.Context, _ anthropic.BetaMessageNewParams, sink api.StreamSink) (anthropic.BetaMessage, error) {
	if sink.OnNotice != nil {
		sink.OnNotice("moved to the fallback model")
	}
	return anthropic.BetaMessage{StopReason: "end_turn"}, nil
}

// A provider notice must reach the frontend as its own event, not be lost and
// not be mixed into the assistant's reply.
func TestProviderNoticeBecomesNoticeEvent(t *testing.T) {
	loop := New(noticeProvider{}, tools.NewRegistry())
	var notices, assistant []string
	if _, err := loop.Run(context.Background(), Options{Prompt: "hi"}, func(e Event) {
		switch e.Type {
		case "notice":
			notices = append(notices, e.Content)
		case "assistant":
			assistant = append(assistant, e.Text)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || notices[0] != "moved to the fallback model" {
		t.Errorf("notices = %q, want the provider's one line", notices)
	}
	if len(assistant) != 0 {
		t.Errorf("the notice leaked into the reply: %q", assistant)
	}
}
