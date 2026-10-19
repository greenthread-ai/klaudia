package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/api"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

// paramsProvider captures the whole request, not just the messages, so a test
// can assert on what was configured rather than only on what was said.
type paramsProvider struct {
	last anthropic.BetaMessageNewParams
}

func (p *paramsProvider) StreamTurn(_ context.Context, params anthropic.BetaMessageNewParams, _ api.StreamSink) (anthropic.BetaMessage, error) {
	p.last = params
	return anthropic.BetaMessage{StopReason: "end_turn"}, nil
}

func TestLoopSendsEffort(t *testing.T) {
	tests := []struct {
		name string
		set  api.Effort
		want anthropic.BetaOutputConfigEffort
	}{
		{name: "unset leaves output_config empty", set: api.EffortUnset, want: ""},
		{name: "low", set: api.EffortLow, want: "low"},
		{name: "xhigh", set: api.EffortXHigh, want: "xhigh"},
		{name: "max", set: api.EffortMax, want: "max"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &paramsProvider{}
			loop := New(p, tools.NewRegistry())
			if _, err := loop.Run(context.Background(), Options{
				Prompt: "hello",
				Model:  "claude-opus-5",
				Effort: tt.set,
			}, nil); err != nil {
				t.Fatal(err)
			}
			if got := p.last.OutputConfig.Effort; got != tt.want {
				t.Errorf("output_config.effort = %q, want %q", got, tt.want)
			}
		})
	}
}

// An unset effort has to leave the request exactly as it was before the
// feature existed — output_config is omitzero, so the key must not appear at
// all rather than appear empty.
func TestUnsetEffortOmitsOutputConfig(t *testing.T) {
	p := &paramsProvider{}
	loop := New(p, tools.NewRegistry())
	if _, err := loop.Run(context.Background(), Options{Prompt: "hello", Model: "claude-opus-5"}, nil); err != nil {
		t.Fatal(err)
	}
	body, err := p.last.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "" {
		t.Fatal("empty body")
	}
	for _, key := range []string{"output_config", "effort"} {
		if strings.Contains(string(body), `"`+key+`"`) {
			t.Errorf("body %s should not mention %q when effort is unset", body, key)
		}
	}
}

// A sub-agent runs at the level the spawner was given, not at whatever the
// child loop's zero value happens to be — that is what makes [subagents]
// effort a usable cost control rather than a field nobody reads.
func TestSpawnerPassesEffortToChild(t *testing.T) {
	dir, path := fixtureFile(t)
	for _, tt := range []struct {
		name string
		set  api.Effort
		want anthropic.BetaOutputConfigEffort
	}{
		{name: "inherited level reaches the child", set: api.EffortLow, want: "low"},
		{name: "unset stays unset in the child too", set: api.EffortUnset, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := &paramsProvider{}
			sp := readOnlySpawner(t, p, dir, 1).WithEffort(tt.set)
			if _, err := sp.Spawn(context.Background(), "Explore", "read "+path, nil); err != nil {
				t.Fatal(err)
			}
			if got := p.last.OutputConfig.Effort; got != tt.want {
				t.Errorf("child output_config.effort = %q, want %q", got, tt.want)
			}
		})
	}
}
