package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateConfig(t *testing.T) {
	tests := []struct {
		name      string
		scope     string
		wantUnder func(home, cwd string) string
	}{
		{
			name:  "global",
			scope: "global",
			wantUnder: func(home, cwd string) string {
				return filepath.Join(home, ".klaudia", "config.toml")
			},
		},
		{
			name:  "local",
			scope: "local",
			wantUnder: func(home, cwd string) string {
				return filepath.Join(cwd, ".klaudia", "config.toml")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			cwd := t.TempDir()
			t.Setenv("HOME", home)

			path, err := createConfig(tt.scope, cwd)
			if err != nil {
				t.Fatalf("createConfig() error = %v", err)
			}
			want := tt.wantUnder(home, cwd)
			if path != want {
				t.Fatalf("path = %q, want %q", path, want)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			body := string(data)
			for _, want := range []string{`provider = "openai"`, `baseURL = "https://api.example.com/v1"`, `apiKeyEnv = "MY_API_KEY"`, `# Klaudia config`} {
				if !strings.Contains(body, want) {
					t.Errorf("starter config missing %s:\n%s", want, body)
				}
			}

			if _, err := createConfig(tt.scope, cwd); err == nil || !strings.Contains(err.Error(), "config already exists") {
				t.Fatalf("second createConfig() error = %v, want already exists", err)
			}
		})
	}
}

func TestCreateConfigRejectsInvalidScope(t *testing.T) {
	if _, err := createConfig("project", t.TempDir()); err == nil || !strings.Contains(err.Error(), "global or local") {
		t.Fatalf("createConfig invalid scope error = %v", err)
	}
}

func TestThemeOrWarn(t *testing.T) {
	var warned []string
	warn := func(m string) { warned = append(warned, m) }

	// Empty → passes through, no warning.
	if got := themeOrWarn("", warn); got != "" || len(warned) != 0 {
		t.Errorf("empty theme: got %q, warnings %v", got, warned)
	}
	// Known theme → passes through, no warning.
	if got := themeOrWarn("nord", warn); got != "nord" || len(warned) != 0 {
		t.Errorf("known theme: got %q, warnings %v", got, warned)
	}
	// Unknown → falls back to default ("") and warns.
	if got := themeOrWarn("bogus", warn); got != "" {
		t.Errorf("unknown theme: got %q, want fallback to default", got)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "unknown theme") {
		t.Errorf("expected an unknown-theme warning, got %v", warned)
	}
}
