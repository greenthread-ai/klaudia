package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/api"
)

func TestLoginNonInteractiveWritesStore(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", cfg)
	// Isolate credential resolution from the ambient environment.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"login", "--api-key", "sk-ant-cli"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("login: %v", err)
	}

	path := filepath.Join(cfg, "credentials.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credentials: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
	if !strings.Contains(out.String(), path) {
		t.Errorf("output %q does not mention %q", out.String(), path)
	}

	// The stored key is what ResolveCredential now returns.
	cred, err := api.ResolveCredential()
	if err != nil {
		t.Fatalf("ResolveCredential after login: %v", err)
	}
	if cred.APIKey != "sk-ant-cli" || cred.IsOAuth() {
		t.Errorf("cred = %+v, want APIKey sk-ant-cli", cred)
	}
}

func TestLoginPipedStdinReadsKey(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", cfg)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("sk-ant-piped\n"))
	cmd.SetArgs([]string{"login"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("login: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cfg, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "sk-ant-piped") {
		t.Errorf("stored file %q missing key", data)
	}
}
