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

// Report, not assert: these are the causes being measured. Each one that
// prints is a place the harness drops the model's earlier reasoning.
func TestAppendOnlyMeasure(t *testing.T) {
	run := func(name string, reg *tools.Registry, opts Options, turns ...anthropic.BetaMessage) {
		p := &requestLog{turns: turns}
		opts.Prompt, opts.Model = "go", "claude-opus-5-5"
		New(p, reg).Run(context.Background(), opts, nil)
		t.Logf("%-28s %d requests, breaks: %q", name, len(p.reqs), appendOnlyBreaks(p.reqs))
	}

	// A ToolSearch reveal adds a deferred tool's definition to the next request.
	run("ToolSearch reveal",
		tools.NewRegistry(hookTool{name: "Probe", out: "ok", fn: func(c tools.Context) { c.Reveal("Later") }}, hookTool{name: "Later", out: "ok"}),
		Options{DeferredTools: map[string]bool{"Later": true}},
		thinkingToolTurn(t, "a", "Probe"), thinkingToolTurn(t, "b", "Later"))

	// Leaving plan mode mid-run (an approved plan) rebuilds the system prompt.
	mode := permission.ModePlan
	run("plan mode -> autonomous",
		tools.NewRegistry(hookTool{name: "Probe", out: "ok", fn: func(tools.Context) { mode = permission.ModeAutonomous }}),
		Options{Permission: permission.Context{Mode: func() permission.Mode { return mode }}},
		thinkingToolTurn(t, "a", "Probe"), thinkingToolTurn(t, "b", "Probe"))

	// Microcompact elides old tool results once they pass its threshold.
	big := strings.Repeat("x", 4*25000)
	var turns []anthropic.BetaMessage
	for i := 0; i < 6; i++ {
		turns = append(turns, thinkingToolTurn(t, fmt.Sprint("m", i), "Big"))
	}
	run("microcompact", tools.NewRegistry(hookTool{name: "Big", out: big}), Options{ContextWindow: 10_000_000}, turns...)
}
