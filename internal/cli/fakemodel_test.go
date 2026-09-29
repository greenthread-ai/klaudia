package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/fakeapi"
)

// This file drives run() in-process: the real cobra command, real config
// loading, real tools and transcripts, against a scripted Messages API on a
// loopback port. It is the unit-level twin of the e2e harness (e2e/), which
// runs the built binary; running the same wiring in the test process is what
// lets a failure here point at a line of root.go.

// The scripted API is internal/fakeapi, shared with the e2e suite.
type (
	fakeTurn  = fakeapi.Turn
	fakeModel = fakeapi.Server
)

var (
	say = fakeapi.Say
	use = fakeapi.Use
)

func newFakeModel(t *testing.T, script ...fakeTurn) *fakeModel { return fakeapi.New(t, script...) }

// cliEnv is an isolated place to run the command in-process: HOME, config dir
// and working directory are temp directories, the credential is fake, and the
// API endpoint is the fake model (nil model: no endpoint).
type cliEnv struct {
	t    *testing.T
	Home string
	Dir  string
}

func newCLIEnv(t *testing.T, m *fakeModel) *cliEnv {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &cliEnv{t: t, Home: filepath.Join(root, "home"), Dir: filepath.Join(root, "project")}
	for _, d := range []string{e.Home, e.Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", e.Home)
	t.Setenv("KLAUDIA_CONFIG_DIR", filepath.Join(e.Home, ".klaudia"))
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-unit-fake")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("KLAUDIA_MAX_RETRIES", "0")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "unit")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "unit@example.invalid")
	}
	endpoint := ""
	if m != nil {
		endpoint = m.URL()
	}
	t.Setenv("KLAUDIA_CUSTOM_ENDPOINT", endpoint)
	t.Chdir(e.Dir)
	return e
}

// write creates a file under the project directory.
func (e *cliEnv) write(rel, content string) {
	e.t.Helper()
	p := filepath.Join(e.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

// cliResult is a finished in-process run.
type cliResult struct {
	Err    error
	Code   int
	Stdout string
	Stderr string
}

func (r cliResult) dump() string {
	return fmt.Sprintf("err=%v code=%d\n--- stdout ---\n%s\n--- stderr ---\n%s", r.Err, r.Code, r.Stdout, r.Stderr)
}

// lines decodes stdout as JSON lines.
func (r cliResult) lines() []map[string]any {
	var out []map[string]any
	for _, l := range strings.Split(r.Stdout, "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func (r cliResult) result() map[string]any {
	var last map[string]any
	for _, l := range r.lines() {
		if l["type"] == "result" {
			last = l
		}
	}
	return last
}

// run executes the root command with args and stdin, collecting its output.
func (e *cliEnv) run(stdin io.Reader, args ...string) cliResult {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	err := e.runTo(stdin, &stdout, &stderr, args...)
	return cliResult{Err: err, Code: exitCodeFor(err), Stdout: stdout.String(), Stderr: stderr.String()}
}

// runTo executes the root command with caller-supplied streams, for runs that
// must be read while they are still going.
func (e *cliEnv) runTo(stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	cmd := NewRootCommand()
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	cmd.SetIn(stdin)
	return cmd.ExecuteContext(context.Background())
}
