package cli

import (
	"testing"

	"github.com/greenthread-ai/klaudia/internal/config"
)

func TestResolveEndpoint(t *testing.T) {
	const (
		envURL = "https://env.example"
		cfgURL = "https://config.example/v1"
	)
	tests := []struct {
		name string
		env  string
		cfg  config.Config
		want string
	}{
		{
			name: "nothing set",
			want: "",
		},
		{
			// Previously the only way to point the Anthropic client anywhere
			// else, and it stays the highest-precedence override.
			name: "env alone",
			env:  envURL,
			want: envURL,
		},
		{
			name: "env beats config, as everywhere else",
			env:  envURL,
			cfg:  config.Config{BaseURL: cfgURL},
			want: envURL,
		},
		{
			// The env var used to be ignored on this path entirely: the
			// OpenAI provider read cfg.BaseURL and nothing else.
			name: "env applies to the openai provider too",
			env:  envURL,
			cfg:  config.Config{Provider: config.ProviderOpenAI, BaseURL: cfgURL},
			want: envURL,
		},
		{
			name: "config when no env",
			cfg:  config.Config{BaseURL: cfgURL},
			want: cfgURL,
		},
		{
			name: "empty env does not shadow config",
			env:  "",
			cfg:  config.Config{BaseURL: cfgURL},
			want: cfgURL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KLAUDIA_CUSTOM_ENDPOINT", tt.env)
			if got := resolveEndpoint(tt.cfg); got != tt.want {
				t.Errorf("resolveEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The OpenAI provider requires an endpoint; it must accept one that arrived
// from the env var, not only from cfg.BaseURL.
func TestBuildProviderEndpointSources(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		cfg     config.Config
		wantErr bool
	}{
		{
			name:    "openai with no endpoint anywhere is rejected",
			cfg:     config.Config{Provider: config.ProviderOpenAI, APIKey: "k"},
			wantErr: true,
		},
		{
			name: "openai satisfied by the config key",
			cfg:  config.Config{Provider: config.ProviderOpenAI, APIKey: "k", BaseURL: "https://x/v1"},
		},
		{
			name: "openai satisfied by the env var",
			env:  "https://x/v1",
			cfg:  config.Config{Provider: config.ProviderOpenAI, APIKey: "k"},
		},
		{
			name:    "openai endpoint present but key missing still fails",
			cfg:     config.Config{Provider: config.ProviderOpenAI, BaseURL: "https://x/v1"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KLAUDIA_CUSTOM_ENDPOINT", tt.env)
			_, _, err := buildProvider(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
