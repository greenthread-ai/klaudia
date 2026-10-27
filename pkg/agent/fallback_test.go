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

// overloadedProvider refuses every request to the models in down with 529
// and answers the rest, recording which model each request named.
type overloadedProvider struct {
	down  map[anthropic.Model]bool
	asked []anthropic.Model
}

func (p *overloadedProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.asked = append(p.asked, params.Model)
	if p.down[params.Model] {
		return anthropic.BetaMessage{}, &api.OpenAIError{StatusCode: 529, Body: `{"error":{"type":"overloaded_error","message":"Overloaded"}}`}
	}
	var m anthropic.BetaMessage
	err := json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"answered"}]}`), &m)
	return m, err
}

// An overloaded model hands the rest of the Run to the next in the chain, and
// says so; with no chain the error stands, and a 4xx never falls back.
func TestFallbackModelChain(t *testing.T) {
	p := &overloadedProvider{down: map[anthropic.Model]bool{"claude-opus-5-5": true, "claude-sonnet-5-5": true}}
	l := New(p, tools.NewRegistry())
	var notices []string
	res, err := l.Run(context.Background(), Options{
		Model: "claude-opus-5-5", Prompt: "hi", Effort: api.EffortMax,
		FallbackModels: []anthropic.Model{"claude-sonnet-5-5", "claude-haiku-5-5"},
	}, func(e Event) {
		if e.Type == EventNotice {
			notices = append(notices, e.Content)
		}
	})
	if err != nil || res.Text != "answered" {
		t.Fatalf("Run = %q, %v", res.Text, err)
	}
	if got := p.asked; len(got) != 3 || got[2] != "claude-haiku-5-5" {
		t.Errorf("models asked = %v", got)
	}
	if len(notices) != 2 || !strings.Contains(notices[1], "continuing on claude-haiku-5-5") {
		t.Errorf("notices = %q", notices)
	}

	p2 := &overloadedProvider{down: map[anthropic.Model]bool{"claude-opus-5-5": true}}
	if _, err := New(p2, tools.NewRegistry()).Run(context.Background(), Options{Model: "claude-opus-5-5", Prompt: "hi"}, nil); err == nil {
		t.Error("with no chain, the overload should be reported")
	}
	if api.IsUnavailable(&api.OpenAIError{StatusCode: 400}) || api.IsUnavailable(&api.OpenAIError{StatusCode: 429}) {
		t.Error("a 4xx is the request, not the model")
	}
}
