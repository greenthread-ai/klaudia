package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/tools"
	"github.com/greenthread-ai/klaudia/pkg/api"
	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// Preserved thinking (PR #63) holds when every request is the previous one
// with messages appended: the same system prompt, the same tool set (as a
// name-sorted set), and every earlier message unchanged; cache_control
// markers may move. These tests run scenarios through the real loop and diff
// consecutive requests the way the API's check does, so each harness edit
// that would drop the model's earlier reasoning shows up here, offline.

// requestLog answers from a script and keeps every request.
type requestLog struct {
	turns []anthropic.BetaMessage
	reqs  []anthropic.BetaMessageNewParams
}

func (p *requestLog) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.reqs = append(p.reqs, params)
	if len(p.reqs) > len(p.turns) {
		var m anthropic.BetaMessage
		json.Unmarshal([]byte(`{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"done"}]}`), &m)
		return m, nil
	}
	return p.turns[len(p.reqs)-1], nil
}

// thinkingToolTurn is a tool_use turn that thinks first, as Opus 5.5 does.
func thinkingToolTurn(t *testing.T, id, name string) anthropic.BetaMessage {
	t.Helper()
	raw := fmt.Sprintf(`{"role":"assistant","stop_reason":"tool_use","content":[
		{"type":"thinking","thinking":"","signature":"sig-%s"},
		{"type":"tool_use","id":%q,"name":%q,"input":{}}]}`, id, id, name)
	var m anthropic.BetaMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// hookTool runs fn when called and returns out.
type hookTool struct {
	name string
	out  string
	fn   func(tools.Context)
}

func (h hookTool) Name() string                                { return h.name }
func (h hookTool) Description(context.Context) (string, error) { return h.name, nil }
func (h hookTool) InputSchema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (h hookTool) ValidateInput(json.RawMessage) error         { return nil }
func (h hookTool) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}
func (h hookTool) CheckPermissions(permission.Context, permission.PermissionRequest) permission.Decision {
	return permission.Decision{Behavior: permission.Allow}
}
func (h hookTool) Execute(_ context.Context, tctx tools.Context, _ json.RawMessage) ([]tools.Result, error) {
	if h.fn != nil {
		h.fn(tctx)
	}
	return []tools.Result{{Content: h.out}}, nil
}

// canonical marshals v with every cache_control key removed.
func canonical(v any) string {
	b, _ := json.Marshal(v)
	var x any
	json.Unmarshal(b, &x)
	var strip func(any) any
	strip = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			delete(t, "cache_control")
			for k, v := range t {
				t[k] = strip(v)
			}
		case []any:
			for i := range t {
				t[i] = strip(t[i])
			}
		}
		return x
	}
	out, _ := json.Marshal(strip(x))
	return string(out)
}

func toolNames(p anthropic.BetaMessageNewParams) []string {
	var out []string
	for _, t := range p.Tools {
		if t.OfTool != nil {
			out = append(out, canonical(t.OfTool))
		}
	}
	sort.Strings(out)
	return out
}

// appendOnlyBreaks lists, for each consecutive pair of requests, what the
// later one changed that preserved thinking does not allow.
func appendOnlyBreaks(reqs []anthropic.BetaMessageNewParams) []string {
	var breaks []string
	for i := 1; i < len(reqs); i++ {
		prev, next := reqs[i-1], reqs[i]
		if canonical(prev.System) != canonical(next.System) {
			breaks = append(breaks, fmt.Sprintf("request %d: system prompt changed", i+1))
		}
		if !reflect.DeepEqual(toolNames(prev), toolNames(next)) {
			breaks = append(breaks, fmt.Sprintf("request %d: tool set changed", i+1))
		}
		for j := range prev.Messages {
			if j >= len(next.Messages) || canonical(prev.Messages[j]) != canonical(next.Messages[j]) {
				breaks = append(breaks, fmt.Sprintf("request %d: message %d changed", i+1, j))
				break
			}
		}
	}
	return breaks
}

func TestAppendOnlyPlainToolTurns(t *testing.T) {
	p := &requestLog{turns: []anthropic.BetaMessage{thinkingToolTurn(t, "a", "Probe"), thinkingToolTurn(t, "b", "Probe")}}
	New(p, tools.NewRegistry(hookTool{name: "Probe", out: "ok"})).Run(context.Background(), Options{Prompt: "go", Model: "claude-opus-5-5"}, nil)
	if b := appendOnlyBreaks(p.reqs); len(b) != 0 {
		t.Errorf("plain tool turns broke append-only: %q", b)
	}
}

// Leaving plan mode mid-run (an approved plan) used to rebuild the system
// prompt; the mode is now told in an appended message (mode.go).
func TestAppendOnlyAcrossAModeChange(t *testing.T) {
	mode := permission.ModePlan
	p := &requestLog{turns: []anthropic.BetaMessage{thinkingToolTurn(t, "a", "Probe"), thinkingToolTurn(t, "b", "Probe")}}
	New(p, tools.NewRegistry(hookTool{name: "Probe", out: "ok", fn: func(tools.Context) { mode = permission.ModeAutonomous }})).Run(
		context.Background(), Options{Prompt: "go", Model: "claude-opus-5-5", Permission: permission.Context{Mode: func() permission.Mode { return mode }}}, nil)
	if b := appendOnlyBreaks(p.reqs); len(b) != 0 {
		t.Errorf("a mode change broke append-only: %q", b)
	}
}

// A ToolSearch reveal used to add the tool's definition to the next request,
// and the set began empty on every Run, so the next prompt took it away
// again. The tools array now carries every tool from the first request and
// the reveal is a marker in the history (api/toolreveal.go).
func TestAppendOnlyAcrossAToolSearchReveal(t *testing.T) {
	p := &requestLog{turns: []anthropic.BetaMessage{thinkingToolTurn(t, "a", "Probe"), thinkingToolTurn(t, "b", "Later")}}
	reg := tools.NewRegistry(hookTool{name: "Probe", out: "ok", fn: func(c tools.Context) { c.Reveal("Later") }}, hookTool{name: "Later", out: "ok"})
	opts := Options{Prompt: "go", Model: "claude-opus-5-5", DeferredTools: map[string]bool{"Later": true}}
	res, err := New(p, reg).Run(context.Background(), opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A second prompt, as the TUI sends it: a new Run over the history.
	opts.Prompt, opts.InitialMessages = "again", res.Messages
	if _, err := New(p, reg).Run(context.Background(), opts, nil); err != nil {
		t.Fatal(err)
	}
	if b := appendOnlyBreaks(p.reqs); len(b) != 0 {
		t.Errorf("a reveal broke append-only: %q", b)
	}
	last := p.reqs[len(p.reqs)-1]
	if got := api.RevealedTools(last.Messages); !got["Later"] || len(got) != 1 {
		t.Errorf("revealed in the next run's history = %v, want Later", got)
	}
}

// clearingLog is a requestLog whose provider clears old tool results
// server-side, as the Anthropic client does once a conversation holds thinking.
type clearingLog struct{ requestLog }

func (*clearingLog) ClearsToolResults([]anthropic.BetaMessageParam) bool { return true }

// Microcompact edits old tool results in place, which broke the chain; with a
// provider that clears server-side it is left alone. A provider that does not
// (the OpenAI-compatible one) keeps microcompact. (PR #63.)
func TestAppendOnlyWhenTheServerClearsToolResults(t *testing.T) {
	big := strings.Repeat("x", 4*25000)
	var turns []anthropic.BetaMessage
	for i := 0; i < 6; i++ {
		turns = append(turns, thinkingToolTurn(t, fmt.Sprint("m", i), "Big"))
	}
	reg := func() *tools.Registry { return tools.NewRegistry(hookTool{name: "Big", out: big}) }
	opts := Options{Prompt: "go", Model: "claude-opus-5-5", ContextWindow: 10_000_000}

	c := &clearingLog{requestLog{turns: turns}}
	New(c, reg()).Run(context.Background(), opts, nil)
	if b := appendOnlyBreaks(c.reqs); len(b) != 0 {
		t.Errorf("with server-side clearing, history was still edited: %q", b)
	}
	p := &requestLog{turns: turns}
	New(p, reg()).Run(context.Background(), opts, nil)
	if b := appendOnlyBreaks(p.reqs); len(b) == 0 {
		t.Error("without server-side clearing microcompact should still run (and edit)")
	}
}
