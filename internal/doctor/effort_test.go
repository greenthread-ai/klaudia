package doctor

import (
	"strings"
	"testing"
)

func TestEffortCheck(t *testing.T) {
	tests := []struct {
		name       string
		effort     string
		maxOutput  int
		wantCheck  bool
		wantStatus string
		contains   []string
	}{
		{
			name:      "unset effort is not reported at all",
			effort:    "",
			maxOutput: 128000,
		},
		{
			name:       "a set level is reported with the cap it has to fit in",
			effort:     "high",
			maxOutput:  128000,
			wantCheck:  true,
			wantStatus: StatusInfo,
			contains:   []string{"effort high", "128K max output"},
		},
		{
			// The quiet failure: max_tokens bounds thinking and text together,
			// so max against the 8192 floor an unknown model gets truncates
			// mid-thought instead of erroring.
			name:       "max against the unknown-model floor is a warning",
			effort:     "max",
			maxOutput:  8192,
			wantCheck:  true,
			wantStatus: StatusWarn,
			contains:   []string{"bounds thinking and text together", "raise maxTokens"},
		},
		{
			name:       "xhigh against the floor is the same trap",
			effort:     "xhigh",
			maxOutput:  8192,
			wantCheck:  true,
			wantStatus: StatusWarn,
		},
		{
			name:       "high against the floor is not flagged",
			effort:     "high",
			maxOutput:  8192,
			wantCheck:  true,
			wantStatus: StatusInfo,
		},
		{
			name:       "max with room is fine",
			effort:     "max",
			maxOutput:  128000,
			wantCheck:  true,
			wantStatus: StatusInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *Check
			for _, c := range Run(Input{Effort: tt.effort, MaxOutputTokens: tt.maxOutput}) {
				if c.Name == "effort" {
					got = &c
					break
				}
			}
			if !tt.wantCheck {
				if got != nil {
					t.Fatalf("unexpected effort check: %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("no effort check produced")
			}
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (%s)", got.Status, tt.wantStatus, got.Detail)
			}
			for _, want := range tt.contains {
				if !strings.Contains(got.Detail, want) {
					t.Errorf("detail %q should contain %q", got.Detail, want)
				}
			}
		})
	}
}
