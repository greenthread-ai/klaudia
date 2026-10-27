package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// convProbe records the Conversation a tool is handed.
type convProbe struct{ got any }

func (*convProbe) Name() string                                { return "Probe" }
func (*convProbe) Description(context.Context) (string, error) { return "", nil }
func (*convProbe) InputSchema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (*convProbe) ValidateInput(json.RawMessage) error         { return nil }
func (*convProbe) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (*convProbe) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (p *convProbe) Execute(_ context.Context, tctx tools.Context, _ json.RawMessage) ([]tools.Result, error) {
	p.got = tctx.Conversation
	return []tools.Result{{Content: "ok"}}, nil
}

// A tool is handed the conversation including the turn that called it —
// what a fork starts from.
func TestToolsSeeTheConversationThroughTheirTurn(t *testing.T) {
	probe := &convProbe{}
	p := &scriptedProvider{turns: []anthropic.BetaMessage{toolTurn(t, "p1", "Probe", "{}")}}
	l := New(p, tools.NewRegistry(probe))
	prior := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("earlier question")),
		assistantText("earlier answer"),
	}
	if _, err := l.Run(context.Background(), Options{Prompt: "now fork", InitialMessages: prior}, nil); err != nil {
		t.Fatal(err)
	}
	conv, ok := probe.got.([]anthropic.BetaMessageParam)
	if !ok || len(conv) != 4 {
		t.Fatalf("Conversation = %T len %d, want the 3 messages and the turn that made the call", probe.got, len(conv))
	}
	if b, _ := json.Marshal(conv[3]); conv[3].Role != anthropic.BetaMessageParamRoleAssistant || !strings.Contains(string(b), `"p1"`) {
		t.Errorf("last message = %s, want the calling turn", b)
	}
}

// firstRequest records the messages of the first request it sees.
type firstRequest struct{ msgs []anthropic.BetaMessageParam }

func (p *firstRequest) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	if p.msgs == nil {
		p.msgs = params.Messages
	}
	var m anthropic.BetaMessage
	err := json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"forked answer"}]}`), &m)
	return m, err
}

// A fork's first request carries the parent's conversation, answers the
// calling turn's tool calls, and puts the task after them — not in the same
// message as the user's original instruction, which a live fork obeyed.
func TestSpawnForkStartsFromTheConversation(t *testing.T) {
	dir, _ := fixtureFile(t)
	p := &firstRequest{}
	s := readOnlySpawner(t, p, dir, 0)
	var calling anthropic.BetaMessageParam
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[
		{"type":"tool_use","id":"a1","name":"Agent","input":{}},
		{"type":"tool_use","id":"r1","name":"Read","input":{}}]}`), &calling); err != nil {
		t.Fatal(err)
	}
	conv := []anthropic.BetaMessageParam{
		anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("we decided on Postgres; now fork a check")),
		calling,
	}
	out, err := s.SpawnFork(context.Background(), "Explore", "check the migrations", conv, nil)
	if err != nil || !strings.Contains(out, "forked answer") {
		t.Fatalf("SpawnFork = %q, %v", out, err)
	}
	if len(p.msgs) != 3 {
		t.Fatalf("first request had %d messages, want instruction, calling turn, results+task", len(p.msgs))
	}
	last, _ := json.Marshal(p.msgs[2])
	for _, want := range []string{`"tool_use_id":"a1"`, "This sub-agent is you", `"tool_use_id":"r1"`, "check the migrations", "You are a fork"} {
		if !strings.Contains(string(last), want) {
			t.Errorf("fork message lacks %q: %s", want, last)
		}
	}
	if first, _ := json.Marshal(p.msgs[0]); strings.Contains(string(first), "check the migrations") {
		t.Error("the task was merged into the user's original instruction")
	}
	if len(conv) != 2 {
		t.Error("the parent's conversation was modified")
	}
}

func assistantText(s string) anthropic.BetaMessageParam {
	return anthropic.BetaMessageParam{Role: anthropic.BetaMessageParamRoleAssistant,
		Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(s)}}
}
