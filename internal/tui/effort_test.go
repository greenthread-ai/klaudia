package tui

import (
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/api"
)

func effortModel(t *testing.T, model string, start api.Effort) *Model {
	t.Helper()
	m := newTestModel()
	m.resize(100, 40)
	m.sess.Model = model
	m.sess.ResolvedModel = string(api.ResolveModel(model))
	m.sess.Effort = start
	return m
}

func TestEffortCommand(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		start    api.Effort
		args     string
		want     api.Effort
		contains []string
	}{
		{
			name:     "no argument lists the levels and marks the live one",
			model:    "claude-opus-5",
			start:    api.EffortHigh,
			want:     api.EffortHigh,
			contains: []string{"low", "medium", "xhigh", "max", "→ high"},
		},
		{
			name:     "no argument with nothing set says so",
			model:    "claude-opus-5",
			args:     "",
			contains: []string{"provider default"},
		},
		{
			name:     "setting a level",
			model:    "claude-opus-5",
			args:     "xhigh",
			want:     api.EffortXHigh,
			contains: []string{"Effort set to xhigh", "next turn"},
		},
		{
			name:  "a change warns about the prompt cache it just invalidated",
			model: "claude-opus-5",
			args:  "low",
			want:  api.EffortLow,
			// The cost is real and otherwise invisible: the next turn re-reads
			// the whole conversation at full input rate.
			contains: []string{"prompt cache"},
		},
		{
			name:     "off restores the provider default",
			model:    "claude-opus-5",
			start:    api.EffortMax,
			args:     "off",
			want:     api.EffortUnset,
			contains: []string{"Effort cleared"},
		},
		{
			name:     "an unknown level is refused with the vocabulary",
			model:    "claude-opus-5",
			start:    api.EffortHigh,
			args:     "turbo",
			want:     api.EffortHigh,
			contains: []string{"unknown effort", "low|medium|high|xhigh|max"},
		},
		{
			name:  "a level this model rejects is caught here, not on the wire",
			model: "claude-opus-4-6",
			args:  "xhigh",
			want:  api.EffortUnset,
			// The API 400s on an unsupported combination rather than dropping
			// to the nearest level, so the next turn would simply die.
			contains: []string{"does not support effort xhigh", "highest: high"},
		},
		{
			name:     "an unknown model is the server's business",
			model:    "openai/gpt-5.5",
			args:     "max",
			want:     api.EffortMax,
			contains: []string{"Effort set to max"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := effortModel(t, tt.model, tt.start)
			var args []string
			if tt.args != "" {
				args = strings.Fields(tt.args)
			}
			m.setEffort(args)

			if m.sess.Effort != tt.want {
				t.Errorf("effort = %q, want %q", m.sess.Effort, tt.want)
			}
			out := visibleText(m.transcript.String())
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

// Switching to a model that cannot do the live level must not leave the
// session holding a setting that will 400 on the next turn.
func TestSetModelLowersUnsupportedEffort(t *testing.T) {
	m := effortModel(t, "claude-opus-5", api.EffortMax)
	msg := m.setModel("claude-opus-4-6", 0)

	if m.sess.Effort != api.EffortHigh {
		t.Errorf("effort = %q, want it lowered to high", m.sess.Effort)
	}
	for _, want := range []string{"does not support effort max", "effort lowered to high"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q should contain %q", msg, want)
		}
	}
}

func TestSetModelKeepsSupportedEffort(t *testing.T) {
	m := effortModel(t, "claude-opus-5", api.EffortXHigh)
	msg := m.setModel("claude-opus-4-8", 0)

	if m.sess.Effort != api.EffortXHigh {
		t.Errorf("effort = %q, want it kept", m.sess.Effort)
	}
	if strings.Contains(msg, "effort") {
		t.Errorf("message %q should not mention effort when nothing changed", msg)
	}
}
