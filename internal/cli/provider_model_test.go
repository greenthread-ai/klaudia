package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// printRun runs one -p turn in cwd and returns stderr.
func printRun(t *testing.T, cwd string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(cwd)
	var errOut bytes.Buffer
	cmd := NewRootCommand()
	cmd.SetArgs(append([]string{"-p", "--permission-mode", "dontAsk"}, args...))
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(io.Discard)
	cmd.SetErr(&errOut)
	err := cmd.ExecuteContext(context.Background())
	return errOut.String(), err
}

// With provider = "openai", --model sonnet used to reach the endpoint as
// claude-sonnet-5 with Claude's 128000-token cap (#125). It is now sent as
// typed, capped at the unknown-model default, with one warning.
func TestOpenAIProviderSendsAliasUnchanged(t *testing.T) {
	fake := &fakeChat{}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	dir := workdir(t, srv.URL)

	stderr, err := printRun(t, dir, "--model", "sonnet", "hi")
	if err != nil {
		t.Fatalf("run: %v\n%s", err, stderr)
	}
	var body struct {
		Model     string `json:"model"`
		MaxTokens int64  `json:"max_completion_tokens"`
	}
	if err := json.Unmarshal([]byte(fake.last()), &body); err != nil {
		t.Fatalf("request body: %v\n%s", err, fake.last())
	}
	if body.Model != "sonnet" {
		t.Errorf("model sent = %q, want sonnet unchanged", body.Model)
	}
	if body.MaxTokens != 8192 {
		t.Errorf("max_completion_tokens = %d, want the unknown-model default 8192", body.MaxTokens)
	}
	if n := strings.Count(stderr, "is a Claude alias"); n != 1 {
		t.Errorf("want exactly one alias warning, got %d:\n%s", n, stderr)
	}
}

// A non-Anthropic provider has no default model to fall back on: sending it
// Claude's default was the same bug in another form.
func TestOpenAIProviderWithoutModelIsAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	cfg := fmt.Sprintf("provider = \"openai\"\nbaseURL = %q\napiKey = \"test\"\n", "http://127.0.0.1:1")
	if err := os.MkdirAll(filepath.Join(dir, ".klaudia"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".klaudia", "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := printRun(t, dir, "hi")
	if err == nil || !strings.Contains(err.Error(), "needs a model") {
		t.Fatalf("err = %v, want a missing-model error", err)
	}
}
