package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
	"github.com/greenthread-ai/klaudia/pkg/tool"
)

// droppingProvider plays the API: on the first request it reports the block
// signed "stale" as dropped, as the client does after resolving the path; it
// records every request it is sent.
type droppingProvider struct {
	mu    sync.Mutex
	turns []anthropic.BetaMessage
	sent  []anthropic.BetaMessageNewParams
}

func (p *droppingProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, sink api.StreamSink) (anthropic.BetaMessage, error) {
	p.mu.Lock()
	n := len(p.sent)
	p.sent = append(p.sent, params)
	p.mu.Unlock()
	if n == 0 && sink.OnTransformations != nil {
		sink.OnTransformations([]api.InputTransformation{{
			Type: "thinking_dropped", Reason: "prefix_binding_mismatch", Path: "messages.1.content.0", Signature: "stale",
		}})
	}
	return p.turns[n], nil
}

func signaturesIn(params anthropic.BetaMessageNewParams) []string {
	var out []string
	for _, m := range params.Messages {
		for _, b := range m.Content {
			if b.OfThinking != nil {
				out = append(out, b.OfThinking.Signature)
			}
		}
	}
	return out
}

// The API drops a mismatched thinking block for one request; the history kept
// it, so it went out again every turn and was dropped again — the
// "N earlier thinking block(s) dropped" notice on every turn of a resumed
// session (measured live: the same block at messages.1.content.0, request
// after request). The loop now removes what the API dropped, once: the next
// request carries no dead block, the blocks around it are untouched, and the
// notice is shown once.
func TestDroppedThinkingIsRemovedFromHistory(t *testing.T) {
	assistantWith := func(sig, text string) anthropic.BetaMessageParam {
		return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaThinkingBlock(sig, "reasoning "+sig), anthropic.NewBetaTextBlock(text),
		}}
	}
	history := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("start")),
		assistantWith("stale", "earlier answer"),
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("and then")),
		assistantWith("fresh", "later answer"),
	}
	var end anthropic.BetaMessage
	_ = json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`), &end)
	p := &droppingProvider{turns: []anthropic.BetaMessage{
		toolUseTurn(t, "tu1", "Probe", map[string]any{}), end,
	}}
	var notices []string
	rec := &dropRecorder{}
	_, err := New(p, tool.NewRegistry(&probeTool{name: "Probe", log: &runLog{}})).Run(context.Background(), Options{
		Prompt:          "continue",
		InitialMessages: history,
		Recorder:        rec,
		Permission:      permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
	}, func(e Event) {
		if e.Type == EventNotice && strings.Contains(e.Content, "thinking") {
			notices = append(notices, e.Content)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.sent) != 2 {
		t.Fatalf("sent %d requests, want 2", len(p.sent))
	}
	if got := strings.Join(signaturesIn(p.sent[0]), ","); got != "stale,fresh" {
		t.Fatalf("first request carried %q, want both blocks", got)
	}
	if got := strings.Join(signaturesIn(p.sent[1]), ","); got != "fresh" {
		t.Errorf("second request carried %q: the dropped block was sent again", got)
	}
	if len(notices) != 1 {
		t.Errorf("notices = %d, want 1: %q", len(notices), notices)
	}
	// On disk as well: the drops in resumed sessions went undiagnosed for days
	// because no transcript said they had happened.
	if len(rec.notes) != 1 || rec.notes[0] != notices[0] {
		t.Errorf("transcript notes = %q, want the one notice", rec.notes)
	}
}

type dropRecorder struct{ notes []string }

func (r *dropRecorder) Record(string, json.RawMessage) error { return nil }
func (r *dropRecorder) RecordThinkingDropped(note string) error {
	r.notes = append(r.notes, note)
	return nil
}
