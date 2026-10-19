package cli

import (
	"testing"

	"github.com/greenthread-ai/klaudia/internal/api"
)

func TestResolveEffort(t *testing.T) {
	tests := []struct {
		name   string
		flag   string
		env    string
		config string
		want   api.Effort
		errors bool
	}{
		{name: "nothing set stays unset so the provider decides", want: api.EffortUnset},
		{name: "config alone", config: "medium", want: api.EffortMedium},
		{name: "env alone", env: "low", want: api.EffortLow},
		{name: "flag alone", flag: "max", want: api.EffortMax},
		{name: "env beats config so a shell can override a checked-in file", env: "low", config: "max", want: api.EffortLow},
		{name: "flag beats env so one run can override a shell default", flag: "xhigh", env: "low", want: api.EffortXHigh},
		{name: "flag beats both", flag: "high", env: "low", config: "max", want: api.EffortHigh},
		{name: "an empty flag does not mask the config", flag: "", config: "medium", want: api.EffortMedium},
		{name: "a bad flag is a usage error, not a silent fallback", flag: "turbo", errors: true},
		{name: "a bad env var is reported rather than ignored", env: "turbo", errors: true},
		{name: "a bad config value is reported rather than ignored", config: "turbo", errors: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KLAUDIA_EFFORT", tt.env)
			got, err := resolveEffort(tt.flag, tt.config)
			if tt.errors {
				if err == nil {
					t.Fatalf("resolveEffort(%q, %q) = %q, want an error", tt.flag, tt.config, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("resolveEffort(%q, %q) = %q, want %q", tt.flag, tt.config, got, tt.want)
			}
		})
	}
}
