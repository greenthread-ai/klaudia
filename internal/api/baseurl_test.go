package api

import "testing"

func TestResolveAnthropicBaseURL(t *testing.T) {
	cases := []struct {
		name      string
		configURL string
		customEnv string
		anthropic string
		want      string
	}{
		{"default (all unset)", "", "", "", ""},
		{"ANTHROPIC_BASE_URL beats default", "", "", "https://env.example/v1", "https://env.example/v1"},
		{"KLAUDIA_CUSTOM_ENDPOINT beats ANTHROPIC_BASE_URL", "", "https://custom.example", "https://env.example", "https://custom.example"},
		{"explicit config beats every env", "https://cfg.example", "https://custom.example", "https://env.example", "https://cfg.example"},
		{"whitespace-only env is ignored", "", "   ", "  ", ""},
		{"config trimmed", "  https://cfg.example  ", "", "", "https://cfg.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KLAUDIA_CUSTOM_ENDPOINT", tc.customEnv)
			t.Setenv("ANTHROPIC_BASE_URL", tc.anthropic)
			if got := ResolveAnthropicBaseURL(tc.configURL); got != tc.want {
				t.Errorf("ResolveAnthropicBaseURL(%q) = %q, want %q", tc.configURL, got, tc.want)
			}
		})
	}
}
