package doctor

import (
	"strings"
	"testing"
)

// The output cap used to be reported only when an effort level was set, so the
// case that actually caused a failure — a model missing from Klaudia's tables
// asking for the 8192 floor, which truncated a large Write mid-tool-call — was
// invisible on an ordinary run. /doctor exists to show what the next turn will
// do, and the cap is part of that.
func TestMaxOutputCheck(t *testing.T) {
	tests := []struct {
		name       string
		maxOutput  int
		source     string
		wantCheck  bool
		wantStatus string
		contains   []string
	}{
		{
			name:       "a real model limit is reported with its source, no effort set",
			maxOutput:  128000,
			source:     "model default",
			wantCheck:  true,
			wantStatus: StatusInfo,
			contains:   []string{"128K tokens per turn", "model default"},
		},
		{
			name:       "the provider's own figure says so",
			maxOutput:  32768,
			source:     "provider",
			wantCheck:  true,
			wantStatus: StatusInfo,
			contains:   []string{"provider"},
		},
		{
			name:       "the unknown-model floor is a warning with the fix",
			maxOutput:  8192,
			source:     "unknown model — using the safe floor",
			wantCheck:  true,
			wantStatus: StatusWarn,
			contains:   []string{"truncate a large write", "set maxTokens"},
		},
		{
			// A config override at the floor is the user's own choice, but it
			// truncates identically, so it is still worth saying.
			name:       "a configured cap at the floor is still flagged",
			maxOutput:  8192,
			source:     "config override",
			wantCheck:  true,
			wantStatus: StatusWarn,
		},
		{
			name:      "nothing resolved means nothing to report",
			maxOutput: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *Check
			for _, c := range Run(Input{MaxOutputTokens: tt.maxOutput, MaxOutputSource: tt.source}) {
				if c.Name == "max output" {
					got = &c
					break
				}
			}
			if !tt.wantCheck {
				if got != nil {
					t.Fatalf("unexpected max output check: %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("no max output check produced — the cap the turn will request is not reported")
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
