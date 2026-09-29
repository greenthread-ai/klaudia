package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// recordingExecutor answers every Run with a fixed response and remembers the
// requests, so the Bash tool's side of the contract is observable without a
// shell.
type recordingExecutor struct {
	resp sandbox.Response
	err  error
	reqs []sandbox.Request
}

func (e *recordingExecutor) Name() string { return "recording" }
func (e *recordingExecutor) Run(_ context.Context, req sandbox.Request) (sandbox.Response, error) {
	e.reqs = append(e.reqs, req)
	return e.resp, e.err
}
func (e *recordingExecutor) Argv(sandbox.Request) (string, []string) { return "true", nil }

func TestBashRunsInTheWorkingDirWithBoundedTimeout(t *testing.T) {
	cases := []struct {
		name    string
		timeout int
		want    time.Duration
	}{
		{"default", 0, bashDefaultTimeout},
		{"requested", 1500, 1500 * time.Millisecond},
		{"clamped to ten minutes", 60 * 60 * 1000, 10 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ex := &recordingExecutor{resp: sandbox.Response{Stdout: "ok\n"}}
			b, _ := NewBash(ex)
			res := runTool(t, b, Context{WorkingDir: "/work"}, BashInput{Command: "make test", Timeout: c.timeout})
			if res.IsError || res.Content != "ok\n" {
				t.Errorf("res = %+v", res)
			}
			if len(ex.reqs) != 1 {
				t.Fatalf("executor ran %d times", len(ex.reqs))
			}
			r := ex.reqs[0]
			if r.Command != "make test" || r.WorkingDir != "/work" || r.Timeout != c.want {
				t.Errorf("request = %+v, want timeout %v in /work", r, c.want)
			}
		})
	}
}

func TestBashReportsAnExecutorFailure(t *testing.T) {
	ex := &recordingExecutor{err: errors.New("no shell")}
	b, _ := NewBash(ex)
	res := runTool(t, b, Context{}, BashInput{Command: "ls"})
	if !res.IsError || res.Content != "Failed to run command: no shell" {
		t.Errorf("res = %+v", res)
	}
}

// Commands that would sit waiting for a keypress, or that detach a server with
// a bare `&`, are refused before anything runs.
func TestBashRefusesBeforeRunning(t *testing.T) {
	for _, cmd := range []string{
		"vim main.go",
		"git commit",
		"npm run dev &",
	} {
		ex := &recordingExecutor{}
		b, _ := NewBash(ex)
		res := runTool(t, b, Context{}, BashInput{Command: cmd})
		if !res.IsError || res.Content == "" {
			t.Errorf("%q: res = %+v, want a refusal with guidance", cmd, res)
		}
		if len(ex.reqs) != 0 {
			t.Errorf("%q: the executor ran a command that should have been refused", cmd)
		}
	}
}

func TestBashLocalOutputAndExitStatus(t *testing.T) {
	dir := t.TempDir()
	b, _ := NewBash(sandbox.NewLocal())

	res := runTool(t, b, Context{WorkingDir: dir}, BashInput{Command: "pwd; echo err >&2; exit 3"})
	if !res.IsError {
		t.Error("a non-zero exit must mark the result failed")
	}
	for _, want := range []string{dir, "err", "[exit code 3]"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("output %q missing %q", res.Content, want)
		}
	}

	res = runTool(t, b, Context{WorkingDir: dir}, BashInput{Command: "true"})
	if res.IsError || res.Content != "[no output]" {
		t.Errorf("silent success = %+v, want \"[no output]\"", res)
	}
}

func TestBashValidateInput(t *testing.T) {
	b, _ := NewBash(sandbox.NewLocal())
	if err := b.ValidateInput(json.RawMessage(`{"command":"ls"}`)); err != nil {
		t.Errorf("valid command rejected: %v", err)
	}
	if err := b.ValidateInput(json.RawMessage(`{"command":"   "}`)); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("blank command: err = %v", err)
	}
	if err := b.ValidateInput(json.RawMessage(`{}`)); err == nil {
		t.Error("missing command accepted")
	}
}

// An unparseable command line still yields a specifier: the raw command, so a
// rule can still match it exactly.
func TestBashSpecifierFallsBackToTheRawCommand(t *testing.T) {
	b, _ := NewBash(sandbox.NewLocal())
	raw := `echo "unterminated`
	in, _ := json.Marshal(BashInput{Command: raw})
	if got := b.PermissionRequest(in).Specifier; got != raw {
		t.Errorf("specifier = %q, want the raw command", got)
	}
}
