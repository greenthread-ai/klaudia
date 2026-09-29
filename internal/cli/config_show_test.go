package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runRoot executes the root command with args, capturing stdout/stderr and
// returning the resulting exit code (via exitCodeFor, as ExecuteContext would).
func runRoot(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := NewRootCommand()
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errBuf.String(), exitCodeFor(err)
}

func writeProjectConfig(t *testing.T, cwd, body string) {
	t.Helper()
	kd := filepath.Join(cwd, ".klaudia")
	if err := os.MkdirAll(kd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(kd, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestConfigShowRedactsAPIKey(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(cwd)

	const secret = "sk-must-not-appear"
	writeProjectConfig(t, cwd, "provider = \"openai\"\napiKey = \""+secret+"\"\n")

	stdout, _, code := runRoot(t, "config", "show")
	if code != 0 {
		t.Fatalf("config show exit = %d, want 0", code)
	}
	if strings.Contains(stdout, secret) {
		t.Fatalf("config show leaked the apiKey:\n%s", stdout)
	}
	if !strings.Contains(stdout, "apiKey") || !strings.Contains(stdout, "redacted") {
		t.Errorf("config show should note the apiKey is redacted:\n%s", stdout)
	}
	if !strings.Contains(stdout, `provider = "openai"`) {
		t.Errorf("config show should print provider:\n%s", stdout)
	}
}

func TestConfigShowOriginAnnotates(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(cwd)

	// Home sets model; project overrides it — origin annotation should say so.
	if err := os.MkdirAll(filepath.Join(home, ".klaudia"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".klaudia", "config.toml"), []byte("model = \"home-model\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeProjectConfig(t, cwd, "model = \"project-model\"\n")

	stdout, _, code := runRoot(t, "config", "show", "--origin")
	if code != 0 {
		t.Fatalf("config show --origin exit = %d, want 0", code)
	}
	// The model line should carry a project origin annotation.
	var modelLine string
	for _, ln := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "model = ") {
			modelLine = ln
		}
	}
	if modelLine == "" {
		t.Fatalf("no model line in output:\n%s", stdout)
	}
	if !strings.Contains(modelLine, "project-model") || !strings.Contains(modelLine, "# project") {
		t.Errorf("model line = %q, want project value + project origin", modelLine)
	}
}

func TestDoctorExitsNonZeroWithoutCredential(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(cwd)
	// Force an OpenAI provider with no key so no credential resolves, and clear
	// any ambient Anthropic credential so the anthropic path can't resolve one.
	writeProjectConfig(t, cwd, "provider = \"openai\"\nbaseURL = \"https://x.example/v1\"\n")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")

	stdout, _, code := runRoot(t, "doctor")
	if code != ExitError {
		t.Fatalf("doctor exit = %d, want %d (no credential is critical)\n%s", code, ExitError, stdout)
	}
	if !strings.Contains(stdout, "auth") {
		t.Errorf("doctor text output should include the auth check:\n%s", stdout)
	}
}

func TestDoctorJSONShape(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	t.Setenv("HOME", home)
	t.Chdir(cwd)
	writeProjectConfig(t, cwd, "provider = \"openai\"\nbaseURL = \"https://x.example/v1\"\n")
	t.Setenv("ANTHROPIC_API_KEY", "")

	stdout, _, _ := runRoot(t, "doctor", "--json")
	if !strings.Contains(stdout, `"checks"`) || !strings.Contains(stdout, `"ok"`) {
		t.Errorf("doctor --json should emit a checks/ok object:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name"`) || !strings.Contains(stdout, `"status"`) {
		t.Errorf("doctor --json checks should carry name/status:\n%s", stdout)
	}
}
