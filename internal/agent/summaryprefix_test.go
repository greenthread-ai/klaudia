package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/compaction"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// prefixRecordingProvider answers every request with text and records the
// summary requests apart from the ordinary ones.
type prefixRecordingProvider struct {
	summaries, turns []anthropic.BetaMessageNewParams
}

func (p *prefixRecordingProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	last := params.Messages[len(params.Messages)-1].Content
	if len(last) == 1 && last[0].OfText != nil && last[0].OfText.Text == compaction.SummaryInstruction {
		p.summaries = append(p.summaries, params)
	} else {
		p.turns = append(p.turns, params)
	}
	// Built from JSON: finalAssistantText reads through AsText, which a struct
	// literal leaves empty.
	var m anthropic.BetaMessage
	raw, _ := json.Marshal(map[string]any{
		"id": "msg", "type": "message", "role": "assistant", "model": "m", "stop_reason": "end_turn",
		"content": []map[string]any{{"type": "text", "text": "a summary"}},
		"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	return m, nil
}

func toolNames(ts []anthropic.BetaToolUnionParam) []string {
	var names []string
	for _, t := range ts {
		if t.OfTool != nil {
			names = append(names, t.OfTool.Name)
		}
	}
	return names
}

func systemText(s []anthropic.BetaTextBlockParam) string {
	var b strings.Builder
	for _, t := range s {
		b.WriteString(t.Text)
	}
	return b.String()
}

// samePrefix fails the test unless the summary request carries the system
// prompt and tools of the ordinary request: the prompt cache is keyed on
// tools → system → messages, so any difference misses it on the whole history.
func samePrefix(t *testing.T, summary, turn anthropic.BetaMessageNewParams) {
	t.Helper()
	if len(summary.Tools) == 0 {
		t.Error("summary request defines no tools, though the history may carry tool_use blocks")
	}
	if got, want := toolNames(summary.Tools), toolNames(turn.Tools); !reflect.DeepEqual(got, want) {
		t.Errorf("summary tools = %v, want the conversation's %v", got, want)
	}
	if got, want := systemText(summary.System), systemText(turn.System); got != want {
		t.Errorf("summary system = %q, want the conversation's %q", got, want)
	}
}

func readRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	read, err := tools.NewRead()
	if err != nil {
		t.Fatal(err)
	}
	return tools.NewRegistry(read)
}

// Autocompact's summary request used to omit the system prompt and tools, so
// it shared no cached prefix with the conversation and was billed in full.
func TestAutocompactSummaryRequestSharesTheConversationPrefix(t *testing.T) {
	provider := &prefixRecordingProvider{}
	history := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(strings.Repeat("x", 40000)))}
	_, err := New(provider, readRegistry(t)).Run(context.Background(), Options{
		InitialMessages: history,
		Prompt:          "next",
		System:          "you are klaudia",
		ContextWindow:   1000, // far below the history, so autocompact runs
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.summaries) != 1 || len(provider.turns) != 1 {
		t.Fatalf("summaries=%d turns=%d, want one of each", len(provider.summaries), len(provider.turns))
	}
	samePrefix(t, provider.summaries[0], provider.turns[0])
	if systemText(provider.summaries[0].System) != "you are klaudia" {
		t.Errorf("summary system = %q", systemText(provider.summaries[0].System))
	}
}

// /compact reuses the prefix of the last request the loop sent; before any
// request it still defines the tools.
func TestCompactSummaryRequestSharesTheConversationPrefix(t *testing.T) {
	provider := &prefixRecordingProvider{}
	loop := New(provider, readRegistry(t))
	history := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hello"))}

	if _, _, err := loop.Compact(context.Background(), history, "m"); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(provider.summaries[0].Tools); !reflect.DeepEqual(got, []string{"Read"}) {
		t.Errorf("/compact before any turn: tools = %v, want [Read]", got)
	}

	if _, err := loop.Run(context.Background(), Options{Prompt: "hi", System: "you are klaudia", ContextWindow: 1_000_000}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loop.Compact(context.Background(), history, "m"); err != nil {
		t.Fatal(err)
	}
	samePrefix(t, provider.summaries[1], provider.turns[0])
}
