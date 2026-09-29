package e2e

import (
	"strings"
	"testing"
)

// The exit-code contract in internal/cli/exitcode.go, checked on the binary.
// 4 (host change blocked) is covered in permissions_test.go.

func TestExitUsageOnInvalidMode(t *testing.T) {
	e := NewEnv(t, nil)
	if r := e.Run("--permission-mode", "nonsense", "-p", "x"); r.ExitCode != 2 {
		t.Errorf("invalid mode exited %d, want 2\n%s", r.ExitCode, r.dump())
	}
}

func TestExitUsageOnUnknownFlag(t *testing.T) {
	e := NewEnv(t, nil)
	if r := e.Run("--definitely-not-a-flag"); r.ExitCode != 2 {
		t.Errorf("unknown flag exited %d, want 2\n%s", r.ExitCode, r.dump())
	}
}

func TestVersion(t *testing.T) {
	e := NewEnv(t, nil)
	r := e.Run("--version")
	if r.ExitCode != 0 || !strings.Contains(r.Stdout, "klaudia") {
		t.Errorf("--version: %s", r.dump())
	}
}

// Running out of turns with work outstanding is 3, not a generic failure.
func TestExitMaxTurns(t *testing.T) {
	var loop []Turn
	for range 5 {
		loop = append(loop, Use("Bash", map[string]any{"command": "true"}))
	}
	m := NewFakeModel(t, loop...)
	e := NewEnv(t, m)
	r := e.Run("-p", "loop forever", "--dangerously-skip-permissions", "--max-turns", "2",
		"--output-format", "stream-json", "--verbose")
	if r.ExitCode != 3 {
		t.Errorf("exit %d, want 3 (max turns)\n%s", r.ExitCode, r.dump())
	}
	if n := len(m.Requests()); n > 2 {
		t.Errorf("model called %d times with --max-turns 2", n)
	}
}

// An API error ends the run with 1 and an error envelope, rather than a hang
// or a success.
func TestExitErrorOnAPIFailure(t *testing.T) {
	m := NewFakeModel(t, Fail(400, "invalid_request_error"))
	e := NewEnv(t, m)
	r := e.Headless("hi")
	if r.ExitCode != 1 {
		t.Errorf("exit %d, want 1\n%s", r.ExitCode, r.dump())
	}
	if res := r.ResultEvent(); res == nil || res["is_error"] != true {
		t.Errorf("want an is_error result envelope, got %v\n%s", res, r.dump())
	}
}

// A bad credential is reported as such.
func TestExitErrorOnAuthFailure(t *testing.T) {
	m := NewFakeModel(t, Fail(401, "authentication_error"))
	e := NewEnv(t, m)
	r := e.Headless("hi")
	if r.ExitCode != 1 {
		t.Errorf("exit %d, want 1\n%s", r.ExitCode, r.dump())
	}
	out := strings.ToLower(r.Stdout + r.Stderr)
	if !strings.Contains(out, "auth") && !strings.Contains(out, "401") && !strings.Contains(out, "credential") {
		t.Errorf("the failure does not mention authentication\n%s", r.dump())
	}
}

// Flag mistakes are usage errors (2), not run failures (1): nothing ran, and
// the fix is to the command line.
func TestExitUsageOnFlagMistakes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"new-session with continue", []string{"-p", "x", "--new-session", "--continue"}},
		{"new-session with resume", []string{"-p", "x", "--new-session", "--resume", "abc"}},
		{"unknown output format", []string{"-p", "x", "--output-format", "yaml"}},
		{"unknown create-config target", []string{"--create-config", "bogus"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewFakeModel(t)
			e := NewEnv(t, m)
			r := e.Run(tc.args...)
			if r.ExitCode != 2 {
				t.Errorf("exit %d, want 2\n%s", r.ExitCode, r.dump())
			}
			if strings.TrimSpace(r.Stderr) == "" {
				t.Errorf("a usage error printed nothing")
			}
			if n := len(m.Requests()); n != 0 {
				t.Errorf("a usage error still called the model %d times", n)
			}
		})
	}
}
