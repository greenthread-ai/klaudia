package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/compaction"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// textMessage is an end_turn reply carrying text. It is built from JSON because
// the SDK's union accessors (AsText, which finalAssistantText uses) read the
// raw JSON, not the struct fields; a literal would read back as empty.
func textMessage(text string) anthropic.BetaMessage {
	var m anthropic.BetaMessage
	raw, _ := json.Marshal(map[string]any{
		"id": "msg", "type": "message", "role": "assistant", "model": "m", "stop_reason": "end_turn",
		"content": []map[string]any{{"type": "text", "text": text}},
		"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	if err := json.Unmarshal(raw, &m); err != nil {
		panic(err)
	}
	return m
}

// requestChars is the text a request carries, the stand-in for its size.
func requestChars(params anthropic.BetaMessageNewParams) int {
	n := 0
	for _, m := range params.Messages {
		for _, b := range m.Content {
			if b.OfText != nil {
				n += len(b.OfText.Text)
			}
		}
	}
	return n
}

func isSummaryRequest(params anthropic.BetaMessageNewParams) bool {
	last := params.Messages[len(params.Messages)-1]
	return len(last.Content) == 1 && last.Content[0].OfText != nil && last.Content[0].OfText.Text == compaction.SummaryInstruction
}

// sizeLimitedProvider refuses any request over limit characters as too long,
// the way a model with a fixed window does, and records what it was sent.
type sizeLimitedProvider struct {
	limit     int
	summaries []anthropic.BetaMessageNewParams
}

func (p *sizeLimitedProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	if isSummaryRequest(params) {
		p.summaries = append(p.summaries, params)
	}
	if requestChars(params) > p.limit {
		return anthropic.BetaMessage{}, errors.New("prompt is too long: 250000 tokens > 200000 maximum")
	}
	return textMessage("summary of the recent work"), nil
}

// The reported dead end: the conversation overflows, compaction sends the same
// over-limit history as its summary request, that overflows too, and the
// session is stuck. The summary request must shrink until it fits.
func TestOverflowRecoversWhenSummaryRequestAlsoOverflows(t *testing.T) {
	var history []anthropic.BetaMessageParam
	for i := 0; i < 12; i++ {
		history = append(history,
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(strings.Repeat("u", 1000))),
			anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
				Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(strings.Repeat("a", 1000))}})
	}
	provider := &sizeLimitedProvider{limit: 15000}
	var summary string
	res, err := New(provider, tools.NewRegistry()).Run(context.Background(), Options{
		InitialMessages: history,
		Prompt:          "carry on",
		ContextWindow:   1_000_000,
		OnSummary:       func(s string) { summary = s },
	}, nil)
	if err != nil {
		t.Fatalf("overflow not recovered: %v", err)
	}
	if res.StopReason != "end_turn" {
		t.Errorf("stop reason %q, want end_turn", res.StopReason)
	}
	if len(provider.summaries) < 2 || requestChars(provider.summaries[len(provider.summaries)-1]) >= requestChars(provider.summaries[0]) {
		t.Fatalf("summary requests: %d; the retry must be smaller than the refused one", len(provider.summaries))
	}
	if !strings.Contains(summary, "oldest") {
		t.Errorf("summary %q does not say part of the conversation was left out", summary)
	}
}

// failingSummaryProvider answers ordinary turns and fails every summary.
type failingSummaryProvider struct{ summaries int }

func (p *failingSummaryProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	if isSummaryRequest(params) {
		p.summaries++
		return anthropic.BetaMessage{}, errors.New("overloaded")
	}
	return textMessage("ok"), nil
}

// A failing automatic compaction used to be retried silently on every turn.
// It is now reported, and after three failures in a row it stops being tried.
func TestAutocompactFailuresAreReportedAndBounded(t *testing.T) {
	provider := &failingSummaryProvider{}
	loop := New(provider, tools.NewRegistry())
	history := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(strings.Repeat("x", 40000)))}
	var reported []string
	for i := 0; i < 5; i++ {
		_, err := loop.Run(context.Background(), Options{
			InitialMessages: history, Prompt: "next", ContextWindow: 1000,
		}, func(e Event) {
			if e.Type == "compaction" && strings.Contains(e.Content, "failed") {
				reported = append(reported, e.Content)
			}
		})
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
	}
	if provider.summaries != maxCompactFailures {
		t.Errorf("summary attempts over 5 turns = %d, want %d", provider.summaries, maxCompactFailures)
	}
	if len(reported) != maxCompactFailures || !strings.Contains(reported[len(reported)-1], "paused") {
		t.Errorf("reported = %q; want each failure reported and the last to say compaction is paused", reported)
	}
}
