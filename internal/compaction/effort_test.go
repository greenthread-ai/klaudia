package compaction

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/api"
)

// A summary is extraction from a transcript already in front of the model, so
// it is capped at medium: the alternative is paying xhigh or max for it on
// every compaction, which on a long session is the most frequent call after
// the turn itself.
func TestSummaryEffort(t *testing.T) {
	tests := []struct {
		name    string
		session api.Effort
		want    api.Effort
	}{
		{name: "unset stays unset rather than opting into effort", session: api.EffortUnset, want: api.EffortUnset},
		{name: "low is kept: compaction is not the exception to a low session", session: api.EffortLow, want: api.EffortLow},
		{name: "medium is already the cap", session: api.EffortMedium, want: api.EffortMedium},
		{name: "high is capped", session: api.EffortHigh, want: api.EffortMedium},
		{name: "xhigh is capped", session: api.EffortXHigh, want: api.EffortMedium},
		{name: "max is capped", session: api.EffortMax, want: api.EffortMedium},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SummaryEffort(tt.session); got != tt.want {
				t.Errorf("SummaryEffort(%q) = %q, want %q", tt.session, got, tt.want)
			}
		})
	}
}

func TestBuildSummaryRequestCarriesEffort(t *testing.T) {
	msgs := []anthropic.BetaMessageParam{anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("work"))}

	req := BuildSummaryRequest(msgs, "claude-opus-5", 4096, api.EffortMax)
	if req.OutputConfig.Effort != "medium" {
		t.Errorf("effort = %q, want medium (the cap)", req.OutputConfig.Effort)
	}
	// The summary instruction is still the last turn — the effort change must
	// not have disturbed the message assembly.
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}

	req = BuildSummaryRequest(msgs, "claude-opus-5", 4096, api.EffortUnset)
	if req.OutputConfig.Effort != "" {
		t.Errorf("effort = %q, want unset", req.OutputConfig.Effort)
	}
}
