package api

import (
	"strings"
	"testing"
)

func TestParseEffort(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    Effort
		wantErr bool
	}{
		{name: "empty clears the setting", in: "", want: EffortUnset},
		{name: "whitespace is not a level", in: "   ", want: EffortUnset},
		{name: "low", in: "low", want: EffortLow},
		{name: "medium", in: "medium", want: EffortMedium},
		{name: "high", in: "high", want: EffortHigh},
		{name: "xhigh", in: "xhigh", want: EffortXHigh},
		{name: "max", in: "max", want: EffortMax},
		{name: "case and padding are forgiven", in: "  XHigh ", want: EffortXHigh},
		{name: "OpenAI's minimal is not one of ours", in: "minimal", wantErr: true},
		{name: "OpenAI's none is not one of ours", in: "none", wantErr: true},
		{name: "a number is not a level", in: "4096", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEffort(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseEffort(%q) = %q, want an error", tt.in, got)
				}
				// The message has to name the alternatives: "unknown effort"
				// alone leaves the user guessing at the vocabulary.
				if !strings.Contains(err.Error(), "low|medium|high|xhigh|max") {
					t.Errorf("error %q should list the levels", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseEffort(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("ParseEffort(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Every settable level needs a description: /effort prints one per line and a
// blank would look like a rendering bug.
func TestEveryEffortLevelIsDescribed(t *testing.T) {
	for _, lvl := range EffortLevels {
		if lvl.Describe() == "" {
			t.Errorf("effort %q has no description", lvl)
		}
	}
}

func TestEffortSupported(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		level       Effort
		wantOK      bool
		wantHighest Effort
	}{
		{name: "opus 5 takes max", model: "claude-opus-5", level: EffortMax, wantOK: true, wantHighest: EffortMax},
		{name: "the opus alias resolves before the lookup", model: "opus", level: EffortXHigh, wantOK: true, wantHighest: EffortMax},
		{name: "opus 4.6 predates xhigh", model: "claude-opus-4-6", level: EffortXHigh, wantHighest: EffortHigh},
		{name: "opus 4.6 still takes high", model: "claude-opus-4-6", level: EffortHigh, wantOK: true, wantHighest: EffortHigh},
		{name: "sonnet 4.6 predates xhigh", model: "claude-sonnet-4-6", level: EffortMax, wantHighest: EffortHigh},
		{name: "sonnet 4.5 has no effort at all", model: "claude-sonnet-4-5-20250929", level: EffortLow, wantHighest: EffortUnset},
		{name: "unset is supported everywhere, including where effort is not", model: "claude-sonnet-4-5-20250929", level: EffortUnset, wantOK: true},
		{name: "an unknown model is the server's business, not ours", model: "openai/gpt-5.5", level: EffortMax, wantOK: true},
		{name: "a future claude snapshot is not refused locally", model: "claude-opus-9", level: EffortMax, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, highest := EffortSupported(tt.model, tt.level)
			if ok != tt.wantOK {
				t.Errorf("EffortSupported(%q, %q) ok = %v, want %v", tt.model, tt.level, ok, tt.wantOK)
			}
			if highest != tt.wantHighest {
				t.Errorf("highest = %q, want %q", highest, tt.wantHighest)
			}
			msg := EffortError(tt.model, tt.level)
			if ok != (msg == "") {
				t.Errorf("EffortError = %q, inconsistent with ok = %v", msg, ok)
			}
		})
	}
}

// The failure this guards: "unsupported" with no ceiling makes the user try
// levels at random to find one that works.
func TestEffortErrorNamesTheCeiling(t *testing.T) {
	msg := EffortError("claude-opus-4-6", EffortMax)
	if !strings.Contains(msg, "highest: high") {
		t.Errorf("EffortError = %q, want it to name the highest supported level", msg)
	}
	msg = EffortError("claude-sonnet-4-5-20250929", EffortLow)
	if !strings.Contains(msg, "does not support the effort parameter") {
		t.Errorf("EffortError = %q, want it to say effort is unavailable", msg)
	}
}

// Every model in the effort table should be one the rest of the package knows
// about, so the three tables stay in step when the lineup moves.
func TestEffortTableTracksTheModelTables(t *testing.T) {
	for model := range modelMaxEffort {
		if _, ok := modelContextWindows[model]; !ok {
			t.Errorf("%s is in modelMaxEffort but not modelContextWindows", model)
		}
	}
}

// The cap brings a level down to it, and an unset level (the provider's
// default) too — unless the model takes no effort at all.
func TestCapEffort(t *testing.T) {
	for _, tc := range []struct {
		model      string
		e, ceiling Effort
		want       Effort
	}{
		{"claude-opus-5-5", EffortMax, EffortHigh, EffortHigh},
		{"claude-opus-5-5", EffortLow, EffortHigh, EffortLow},
		{"claude-opus-5-5", EffortUnset, EffortMedium, EffortMedium},
		{"claude-opus-5-5", EffortUnset, EffortMax, EffortUnset},
		{"claude-opus-5-5", EffortXHigh, EffortUnset, EffortXHigh},
		{"some-model-without-effort", EffortUnset, EffortMedium, EffortUnset},
	} {
		if got := CapEffort(tc.model, tc.e, tc.ceiling); got != tc.want {
			t.Errorf("CapEffort(%s, %q, %q) = %q, want %q", tc.model, tc.e, tc.ceiling, got, tc.want)
		}
	}
}
