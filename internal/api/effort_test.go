package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestParseEffort(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "default": "", "AUTO": "", " High ": "high",
		"low": "low", "medium": "medium", "xhigh": "xhigh", "max": "max",
	} {
		got, err := ParseEffort(in)
		if err != nil || got != want {
			t.Errorf("ParseEffort(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseEffort("extreme"); err == nil {
		t.Error("an unknown level must be refused, not sent to the API")
	}
}

func TestParseThinking(t *testing.T) {
	for in, want := range map[string]string{
		"": ThinkingDefault, "default": ThinkingDefault,
		"adaptive": ThinkingAdaptive, "on": ThinkingAdaptive,
		"disabled": ThinkingDisabled, "OFF": ThinkingDisabled,
	} {
		got, err := ParseThinking(in)
		if err != nil || got != want {
			t.Errorf("ParseThinking(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := ParseThinking("enabled"); err == nil {
		t.Error("budgeted thinking is not offered; \"enabled\" must be refused")
	}
}

// reasoningJSON applies the settings to a request for model and returns the
// output_config and thinking fields as they go on the wire ("" when absent).
func reasoningJSON(t *testing.T, model, effort, thinking string) (outputConfig, thinkingCfg string) {
	t.Helper()
	p := anthropic.BetaMessageNewParams{Model: anthropic.Model(model), MaxTokens: 1}
	ApplyReasoning(&p, effort, thinking)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return string(m["output_config"]), string(m["thinking"])
}

func TestApplyReasoningDefaultsSendNothing(t *testing.T) {
	oc, th := reasoningJSON(t, "claude-opus-5", "", "")
	if oc != "" || th != "" {
		t.Errorf("unset settings must leave the request as it was; got output_config=%s thinking=%s", oc, th)
	}
}

func TestApplyReasoningWireShape(t *testing.T) {
	oc, th := reasoningJSON(t, "claude-opus-5", "xhigh", ThinkingAdaptive)
	if oc != `{"effort":"xhigh"}` {
		t.Errorf("output_config = %s", oc)
	}
	if th != `{"type":"adaptive"}` {
		t.Errorf("thinking = %s", th)
	}
	_, th = reasoningJSON(t, "claude-opus-4-8", "", ThinkingDisabled)
	if th != `{"type":"disabled"}` {
		t.Errorf("thinking = %s", th)
	}
}

func TestApplyReasoningAdjustsPerModel(t *testing.T) {
	cases := []struct {
		model, effort, thinking string
		wantEffort              string // "" = no output_config
		wantThinking            string // "" = no thinking field
	}{
		// Effort is rejected outright on these.
		{"claude-haiku-4-5", "high", "", "", ""},
		{"claude-haiku-4-5-20251001", "low", "", "", ""},
		{"claude-sonnet-4-5-20250929", "max", "", "", ""},
		// Levels a model lacks are lowered to its highest.
		{"claude-opus-4-5-20251101", "xhigh", "", "high", ""},
		{"claude-opus-4-5-20251101", "max", "", "high", ""},
		{"claude-opus-4-6", "xhigh", "", "high", ""},
		{"claude-opus-4-6", "max", "", "max", ""},
		{"claude-sonnet-4-6", "xhigh", "", "high", ""},
		// Thinking cannot be switched off on these.
		{"claude-fable-5", "", ThinkingDisabled, "", ""},
		{"claude-fable-5-1", "low", ThinkingDisabled, "low", ""},
		{"claude-opus-5-5", "", ThinkingDisabled, "", ""},
		// Opus 5 allows it only at high or below.
		{"claude-opus-5", "high", ThinkingDisabled, "high", "disabled"},
		{"claude-opus-5", "max", ThinkingDisabled, "max", ""},
		// Unknown models get the settings unchanged.
		{"gpt-5.5", "max", ThinkingDisabled, "max", "disabled"},
	}
	for _, c := range cases {
		oc, th := reasoningJSON(t, c.model, c.effort, c.thinking)
		gotEffort := ""
		if oc != "" {
			var v struct{ Effort string }
			_ = json.Unmarshal([]byte(oc), &v)
			gotEffort = v.Effort
		}
		gotThinking := ""
		if th != "" {
			var v struct{ Type string }
			_ = json.Unmarshal([]byte(th), &v)
			gotThinking = v.Type
		}
		if gotEffort != c.wantEffort || gotThinking != c.wantThinking {
			t.Errorf("%s effort=%q thinking=%q: sent effort=%q thinking=%q; want %q, %q",
				c.model, c.effort, c.thinking, gotEffort, gotThinking, c.wantEffort, c.wantThinking)
		}
	}
}

func TestOpenAIReasoningEffort(t *testing.T) {
	p := NewOpenAIProvider("https://x/v1", "k", nil, nil)
	for effort, want := range map[string]string{
		"": "", "low": "low", "medium": "medium", "high": "high", "xhigh": "high", "max": "high",
	} {
		params := anthropic.BetaMessageNewParams{Model: "gpt-5.5", MaxTokens: 1}
		ApplyReasoning(&params, effort, "")
		req, err := p.translateRequest(params)
		if err != nil {
			t.Fatal(err)
		}
		if req.ReasoningEffort != want {
			t.Errorf("effort %q → reasoning_effort %q, want %q", effort, req.ReasoningEffort, want)
		}
		raw, _ := json.Marshal(req)
		if want == "" && strings.Contains(string(raw), "reasoning_effort") {
			t.Errorf("no effort configured must omit reasoning_effort: %s", raw)
		}
	}
}
