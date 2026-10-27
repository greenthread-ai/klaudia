//go:build darwin || linux

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// klaudiaBin is the binary TestMain built, or "" when the e2e tests are off.
var klaudiaBin, mcpServerBin string

func TestMain(m *testing.M) {
	if os.Getenv("KLAUDIA_E2E") != "1" {
		os.Exit(m.Run()) // the e2e tests skip; the emulator's unit tests still run
	}
	dir, err := os.MkdirTemp("", "klaudia-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e:", err)
		os.Exit(1)
	}
	klaudiaBin = filepath.Join(dir, "klaudia")
	build := exec.Command("go", "build", "-o", klaudiaBin, "github.com/greenthread-ai/klaudia/cmd/klaudia")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	start := time.Now()
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: building klaudia:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	mcpServerBin = filepath.Join(dir, "mcpserver")
	build = exec.Command("go", "build", "-o", mcpServerBin, "github.com/greenthread-ai/klaudia/internal/tui/e2e/testdata/mcpserver")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: building the MCP server:", err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "e2e: built klaudia in %v\n", time.Since(start).Round(time.Millisecond))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// Env is one isolated world: a home directory, a project, and the mock
// provider both configs point at.
type Env struct {
	Home    string // HOME
	Config  string // KLAUDIA_CONFIG_DIR
	Project string // the working directory klaudia starts in
	Tmp     string // TMPDIR
	Mock    *Mock

	prompts int // prompts sent through Prompt
}

// mainGo is the project's one source file; the tools scenario edits it.
const mainGo = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n"

// newEnv skips unless KLAUDIA_E2E=1, then builds the isolated world.
func newEnv(t *testing.T) *Env {
	t.Helper()
	if klaudiaBin == "" {
		t.Skip("set KLAUDIA_E2E=1 to run the end-to-end TUI tests")
	}
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r // macOS: /var is /private/var, and links carry the real path
	}
	e := &Env{
		Home:    filepath.Join(root, "home"),
		Project: filepath.Join(root, "project"),
		Tmp:     filepath.Join(root, "tmp"),
		Mock:    newMock(t, defaultScript),
	}
	// The user config lives at $KLAUDIA_CONFIG_DIR/config.toml, which is also
	// $HOME/.klaudia/config.toml, so either resolution finds the mock.
	e.Config = filepath.Join(e.Home, ".klaudia")
	cfg := fmt.Sprintf("provider = \"openai\"\nbaseURL = %q\nmodel = %q\napiKey = \"mock-key\"\ncontextWindow = 128000\n",
		e.Mock.BaseURL(), mockModel)
	for path, data := range map[string]string{
		filepath.Join(e.Config, "config.toml"):              cfg,
		filepath.Join(e.Project, ".klaudia", "config.toml"): cfg,
		filepath.Join(e.Project, "main.go"):                 mainGo,
		filepath.Join(e.Project, "README.md"):               "# e2e fixture\n",
		filepath.Join(e.Tmp, ".keep"):                       "",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("mock answered %d request(s), for prompts %q", e.Mock.Requests(), e.Mock.Prompts())
		}
		if e.prompts > 0 && e.Mock.Requests() == 0 {
			t.Errorf("isolation: %d prompt(s) sent but the mock received no request — klaudia talked to something else", e.prompts)
		}
	})
	return e
}

// environ is the child's whole environment. It is built, not inherited, so
// no provider key, endpoint override or real config directory can leak in.
func (e *Env) environ() []string {
	return []string{
		"HOME=" + e.Home,
		"KLAUDIA_CONFIG_DIR=" + e.Config,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + e.Tmp,
		"LANG=en_US.UTF-8",
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"KLAUDIA_HYPERLINKS=1",
		"USER=e2e",
	}
}

// Start runs klaudia --new-session in the project at 100×30.
func (e *Env) Start(t *testing.T) *Term {
	t.Helper()
	tm := startTerm(t, klaudiaBin, e.Project, e.environ(), []string{"--new-session"}, 100, 30)
	tm.WaitFor(t, "the input box", 20*time.Second, Snapshot.InputBox)
	return tm
}

// Prompt types text, presses Enter, and fails unless the mock receives a
// completion request for it — the guard against a run reaching a real
// provider.
func (e *Env) Prompt(t *testing.T, tm *Term, text string) {
	t.Helper()
	before := e.Mock.Requests()
	e.prompts++
	tm.Type(text)
	tm.Press(keyEnter)
	deadline := time.Now().Add(15 * time.Second)
	for e.Mock.Requests() == before {
		if time.Now().After(deadline) {
			t.Fatalf("isolation: the mock received no request for %q within 15s\n%s", text, tm.Snapshot())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
