package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// The effort level has to reach both providers from the same field, or the two
// backends drift apart in capability — which is the whole reason effort is set
// on the canonical Anthropic params and translated, rather than configured
// per provider.
func TestTranslateRequestCarriesEffort(t *testing.T) {
	tests := []struct {
		name string
		set  Effort
		want string
	}{
		{name: "unset sends no field at all", set: EffortUnset, want: ""},
		{name: "low", set: EffortLow, want: "low"},
		{name: "medium", set: EffortMedium, want: "medium"},
		{name: "high", set: EffortHigh, want: "high"},
		// xhigh and max exist in the OpenAI-compatible vocabulary too, so they
		// pass through unchanged rather than being clamped to "high".
		{name: "xhigh passes through, not clamped", set: EffortXHigh, want: "xhigh"},
		{name: "max passes through, not clamped", set: EffortMax, want: "max"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &OpenAIProvider{}
			params := anthropic.BetaMessageNewParams{
				Model:     "openai/gpt-5.5",
				MaxTokens: 1024,
				Messages:  []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("hi"))},
			}
			if tt.set != EffortUnset {
				params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(tt.set)
			}

			req, err := p.translateRequest(params)
			if err != nil {
				t.Fatal(err)
			}
			if req.ReasoningEffort != tt.want {
				t.Errorf("reasoning_effort = %q, want %q", req.ReasoningEffort, tt.want)
			}

			// The marshalled body is what the server sees, and an unset effort
			// has to leave it byte-for-byte as it was: endpoints that predate
			// the field (older vLLM, llama.cpp) reject an unknown key outright,
			// so `"reasoning_effort":""` would break every one of them.
			body, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			present := strings.Contains(string(body), "reasoning_effort")
			if present != (tt.want != "") {
				t.Errorf("body %s: reasoning_effort present = %v, want %v", body, present, tt.want != "")
			}
		})
	}
}
