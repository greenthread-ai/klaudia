package config

import "testing"

func TestResolveBaseURL(t *testing.T) {
	const url = "https://generic.example/v1"
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "nothing set means the provider's own default",
			cfg:  Config{},
			want: "",
		},
		{
			// The regression this change is about: baseURL used to be read
			// only under provider="openai", so the default provider ignored
			// it and an Anthropic proxy had nowhere in the file to live.
			name: "baseURL reaches the default (anthropic) provider",
			cfg:  Config{BaseURL: url},
			want: url,
		},
		{
			name: "baseURL reaches the openai provider",
			cfg:  Config{Provider: ProviderOpenAI, BaseURL: url},
			want: url,
		},
		{
			name: "explicit anthropic provider behaves like the default",
			cfg:  Config{Provider: ProviderAnthropic, BaseURL: url},
			want: url,
		},
		{
			name: "whitespace-only is treated as unset",
			cfg:  Config{BaseURL: "   "},
			want: "",
		},
		{
			name: "surrounding whitespace is trimmed",
			cfg:  Config{BaseURL: "  " + url + "\n"},
			want: url,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolveBaseURL(); got != tt.want {
				t.Errorf("ResolveBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Dialect and endpoint are independent keys: the same baseURL must be usable
// under either provider, which is what lets one key serve both backends.
func TestBaseURLIsIndependentOfProvider(t *testing.T) {
	const url = "https://host.example/v1"
	anthropic := Config{BaseURL: url}.ResolveBaseURL()
	openai := Config{Provider: ProviderOpenAI, BaseURL: url}.ResolveBaseURL()
	if anthropic != openai {
		t.Errorf("provider changed the endpoint: anthropic=%q openai=%q", anthropic, openai)
	}
}
