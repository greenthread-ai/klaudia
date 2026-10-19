package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/config"
)

// buildProvider resolves the endpoint once, into `endpoint`, and every arm of
// its switch has to construct its provider from that value rather than from
// cfg.BaseURL — otherwise KLAUDIA_CUSTOM_ENDPOINT is read, reported by
// /doctor, and then ignored when the request is sent.
//
// TestBuildProviderEndpointSources cannot catch that. It asserts only whether
// buildProvider returns an error, and the "openai" arm checks `endpoint`
// before building, so handing cfg.BaseURL to the constructor leaves every case
// there green while the traffic goes to the wrong host. This drives a real
// request through each built provider and asserts which server received it:
// the providers keep baseURL unexported, and where the bytes land is the
// question worth asking anyway.
func TestBuildProviderSendsToTheResolvedEndpoint(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.Config
		// The path that provider's ListModels appends to its base URL.
		wantPath string
	}{
		{
			name:     "openai sends to the env override, not to baseURL",
			cfg:      config.Config{Provider: config.ProviderOpenAI, APIKey: "k", BaseURL: "https://wrong.example/v1"},
			wantPath: "/models",
		},
		{
			name:     "greenthread sends to the env override, not to the console default",
			cfg:      config.Config{Provider: config.ProviderGreenThread, APIKey: "k"},
			wantPath: "/v1/models",
		},
		{
			name:     "greenthread prefers the env override over an explicit baseURL",
			cfg:      config.Config{Provider: config.ProviderGreenThread, APIKey: "k", BaseURL: "https://wrong.example"},
			wantPath: "/v1/models",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case got <- r.URL.Path:
				default:
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer srv.Close()

			t.Setenv("KLAUDIA_CUSTOM_ENDPOINT", srv.URL)
			p, _, err := buildProvider(tt.cfg)
			if err != nil {
				t.Fatalf("buildProvider() error = %v", err)
			}
			list := modelLister(p)
			if list == nil {
				t.Fatalf("provider %T does not list models; this test cannot observe its endpoint", p)
			}
			if _, err := list(context.Background()); err != nil {
				t.Fatalf("ListModels through the built provider: %v", err)
			}

			select {
			case path := <-got:
				if path != tt.wantPath {
					t.Errorf("requested %q, want %q", path, tt.wantPath)
				}
			default:
				t.Fatal("the resolved endpoint received no request: the provider was built from a different base URL")
			}
		})
	}
}
