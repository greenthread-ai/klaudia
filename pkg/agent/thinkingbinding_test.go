package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
)

func TestDroppedThinkingNote(t *testing.T) {
	note := droppedThinkingNote([]api.InputTransformation{
		{Type: "thinking_dropped", Reason: "prefix_binding_mismatch", Path: "messages.7.content.0"},
		{Type: "thinking_dropped", Reason: "prefix_binding_mismatch", Path: "messages.9.content.0"},
		{Type: "thinking_dropped", Reason: "model_binding_mismatch"},
		{Type: "thinking_mismatch_allowed", Reason: "prefix_binding_mismatch"},
	})
	for _, want := range []string{"2 earlier thinking block(s) dropped", "messages.7.content.0", "1 thinking block(s) from another model"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q lacks %q", note, want)
		}
	}
	if droppedThinkingNote([]api.InputTransformation{{Type: "thinking_mismatch_allowed"}}) != "" || droppedThinkingNote(nil) != "" {
		t.Error("nothing dropped, but a note was written")
	}
}

// bindingThenOKProvider refuses the first request the way an enforced account
// does when a replayed thinking block no longer matches its conversation, and
// records whether each request still carried thinking.
type bindingThenOKProvider struct {
	calls    int
	thinking []bool
}

func (p *bindingThenOKProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.calls++
	has := false
	for _, m := range params.Messages {
		for _, b := range m.Content {
			has = has || b.OfThinking != nil
		}
	}
	p.thinking = append(p.thinking, has)
	if p.calls == 1 {
		return anthropic.BetaMessage{}, errors.New("400: messages.1.content.0: Invalid `signature` in `thinking` block. The block is bound to a different conversation.")
	}
	var m anthropic.BetaMessage
	json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`), &m)
	return m, nil
}

// Where drop_block cannot be asked for, the 400 repeats on every resend: the
// loop strips every thinking block (a leading-run removal the check accepts)
// and retries once. (PR #63.)
func TestThinkingBindingErrorIsRecoveredByStripping(t *testing.T) {
	history := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("first")),
		{Role: anthropic.BetaMessageParamRoleAssistant, Content: []anthropic.BetaContentBlockParamUnion{
			anthropic.NewBetaThinkingBlock("sig", ""), anthropic.NewBetaTextBlock("answer"),
		}},
	}
	p := &bindingThenOKProvider{}
	var notices []string
	res, err := New(p, tools.NewRegistry()).Run(context.Background(), Options{InitialMessages: history, Prompt: "next", Model: "claude-opus-5-5"},
		func(e Event) {
			if e.Type == EventNotice {
				notices = append(notices, e.Content)
			}
		})
	if err != nil || res.StopReason != "end_turn" {
		t.Fatalf("Run = %+v, %v; want the retry to finish the turn", res.StopReason, err)
	}
	if len(p.thinking) != 2 || !p.thinking[0] || p.thinking[1] {
		t.Errorf("thinking sent per request = %v, want [true false]", p.thinking)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "earlier reasoning") {
		t.Errorf("notices = %q", notices)
	}
}
