// Package e2e drives the real klaudia binary against a scripted, in-process
// stand-in for the Anthropic Messages API.
//
// This is the middle layer of the test pyramid (see docs/testing.md). Unit tests
// under internal/ exercise packages with in-memory fakes; scripts/smoke.sh runs
// the binary against a live model. Neither checks the thing in between: that
// flag parsing, config loading, the HTTP client, SSE decoding, the agent loop,
// the tools, permissions, transcripts and exit codes compose correctly in the
// shipped binary. Here the model's side is scripted, so every assertion is about
// klaudia's behaviour and none depends on what a model chose to do.
//
// Each run gets its own HOME, KLAUDIA_CONFIG_DIR and working directory, and no
// credential beyond a fake API key, so a developer's ~/.klaudia and ~/.claude
// cannot change the outcome.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/fakeapi"
)

// binPath is the klaudia binary built once by TestMain.
var binPath string

// coverDir, from KLAUDIA_E2E_COVERDIR, turns on coverage for the binary.
var coverDir = os.Getenv("KLAUDIA_E2E_COVERDIR")

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	dir, err := os.MkdirTemp("", "klaudia-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: temp dir:", err)
		return 1
	}
	defer os.RemoveAll(dir)

	binPath = filepath.Join(dir, "klaudia")
	args := []string{"build", "-o", binPath}
	if coverDir != "" {
		// Coverage of the binary itself, merged by `make cover` with the unit
		// profile. Every run writes its counters into coverDir via GOCOVERDIR.
		args = append(args, "-cover", "-coverpkg=github.com/greenthread-ai/klaudia/...")
	}
	build := exec.Command("go", append(args, "../cmd/klaudia")...)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: building klaudia:", err)
		return 1
	}
	return m.Run()
}

// ---- scripted model -------------------------------------------------------

// The fake API lives in internal/fakeapi, shared with the in-process cli
// tests. These aliases keep scenarios reading as a script: Use, Say, Fail.
type (
	Turn      = fakeapi.Turn
	ToolCall  = fakeapi.ToolCall
	Request   = fakeapi.Request
	FakeModel = fakeapi.Server
)

var (
	Say  = fakeapi.Say
	Use  = fakeapi.Use
	Fail = fakeapi.Fail
)

// NewFakeModel starts a scripted Messages API on a loopback port.
func NewFakeModel(t *testing.T, script ...Turn) *FakeModel { return fakeapi.New(t, script...) }

// ---- running the binary ---------------------------------------------------

// Env is an isolated place to run klaudia: its own HOME, config dir and
// project directory.
type Env struct {
	t     *testing.T
	Home  string
	Dir   string // the working directory, i.e. the project
	Model *FakeModel
}

// NewEnv creates an isolated environment wired to model.
func NewEnv(t *testing.T, model *FakeModel) *Env {
	t.Helper()
	root := t.TempDir()
	e := &Env{t: t, Home: filepath.Join(root, "home"), Dir: filepath.Join(root, "project"), Model: model}
	for _, d := range []string{e.Home, e.Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// Path joins elem onto the project directory.
func (e *Env) Path(elem ...string) string {
	return filepath.Join(append([]string{e.Dir}, elem...)...)
}

// Result is a finished klaudia run.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Elapsed  time.Duration
}

// Events decodes stdout as stream-json, skipping non-JSON lines.
func (r Result) Events() []map[string]any {
	var out []map[string]any
	sc := bufio.NewScanner(strings.NewReader(r.Stdout))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var ev map[string]any
		if json.Unmarshal(line, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

// ResultEvent returns the final {"type":"result"} envelope, or nil.
func (r Result) ResultEvent() map[string]any {
	var last map[string]any
	for _, ev := range r.Events() {
		if ev["type"] == "result" {
			last = ev
		}
	}
	return last
}

// runTimeout bounds a single invocation. Every scripted run finishes in well
// under a second; the ceiling is there so a regression that hangs fails the
// test instead of the whole `go test` deadline.
const runTimeout = 60 * time.Second

// Run executes klaudia with args in the environment.
func (e *Env) Run(args ...string) Result {
	e.t.Helper()
	return e.RunWithEnv(nil, args...)
}

// RunWithEnv is Run with extra KEY=VALUE entries appended to the environment;
// a later entry overrides an earlier one, so "ANTHROPIC_API_KEY=" removes the
// fake credential.
func (e *Env) RunWithEnv(env []string, args ...string) Result {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = e.Dir
	cmd.Env = append(e.environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 5 * time.Second

	start := time.Now()
	err := cmd.Run()
	res := Result{Stdout: stdout.String(), Stderr: stderr.String(), Elapsed: time.Since(start)}
	if ctx.Err() != nil {
		e.t.Fatalf("klaudia %q did not finish within %s\nstdout:\n%s\nstderr:\n%s", args, runTimeout, res.Stdout, res.Stderr)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		res.ExitCode = ee.ExitCode()
	} else if err != nil {
		e.t.Fatalf("running klaudia: %v", err)
	}
	return res
}

// Headless runs `klaudia -p prompt` with stream-json output plus extra args.
func (e *Env) Headless(prompt string, extra ...string) Result {
	e.t.Helper()
	args := append([]string{"-p", prompt, "--output-format", "stream-json", "--verbose", "--max-turns", "8"}, extra...)
	return e.Run(args...)
}

// environ builds a minimal environment from scratch rather than filtering the
// parent's, so nothing from the developer's shell (a real API key, a custom
// endpoint, KLAUDIA_* tuning) leaks into the run.
func (e *Env) environ() []string {
	env := []string{
		"HOME=" + e.Home,
		"KLAUDIA_CONFIG_DIR=" + filepath.Join(e.Home, ".klaudia"),
		"PATH=" + os.Getenv("PATH"),
		"TERM=dumb",
		"USER=e2e",
		"LANG=C.UTF-8",
		"TMPDIR=" + e.t.TempDir(),
		"ANTHROPIC_API_KEY=sk-ant-e2e-fake",
		"KLAUDIA_MAX_RETRIES=0",
		// Git in the project directory must not reach for the developer's
		// global config or an editor.
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@example.invalid",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@example.invalid",
	}
	if e.Model != nil {
		env = append(env, "KLAUDIA_CUSTOM_ENDPOINT="+e.Model.URL())
	}
	if coverDir != "" {
		env = append(env, "GOCOVERDIR="+coverDir)
	}
	return env
}

// ---- assertion helpers ----------------------------------------------------

// toolUses returns the names of tool_use blocks in assistant events, in order.
func toolUses(events []map[string]any) []string {
	var names []string
	for _, ev := range events {
		if ev["type"] != "assistant" {
			continue
		}
		msg, _ := ev["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		for _, c := range content {
			block, _ := c.(map[string]any)
			if block["type"] == "tool_use" {
				name, _ := block["name"].(string)
				names = append(names, name)
			}
		}
	}
	return names
}

// toolResults returns tool_result blocks from user events.
func toolResults(events []map[string]any) []map[string]any {
	var out []map[string]any
	for _, ev := range events {
		if ev["type"] != "user" {
			continue
		}
		msg, _ := ev["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		for _, c := range content {
			block, _ := c.(map[string]any)
			if block["type"] == "tool_result" {
				out = append(out, block)
			}
		}
	}
	return out
}

// resultText flattens a tool_result's content to a string.
func resultText(block map[string]any) string {
	switch c := block["content"].(type) {
	case string:
		return c
	case []any:
		var b strings.Builder
		for _, part := range c {
			if m, ok := part.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	}
	return ""
}

func (r Result) dump() string {
	return fmt.Sprintf("exit=%d elapsed=%s\n--- stdout ---\n%s\n--- stderr ---\n%s", r.ExitCode, r.Elapsed, r.Stdout, r.Stderr)
}
