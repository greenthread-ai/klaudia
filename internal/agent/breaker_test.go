package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// A real session reached a state where every Bash call — including `true`,
// `pwd` and `echo hello` — was refused in 0ms for the rest of the run. The
// streak that triggers the directive was only ever cleared by a successful
// execution, and the directive is what prevented one: a latch, not a breaker.
func TestShapeStreakDoesNotLatchTheTool(t *testing.T) {
	proj := t.TempDir()
	reg, bash := testRegistry(t)
	bash.err = "dial tcp: connect: network is unreachable"
	l := New(nil, reg)
	opts := Options{
		WorkingDir: proj,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver:   HeadlessApprover(false),
	}
	failures, streaks := map[string]int{}, map[string]errStreak{}

	run := func(cmd string) string {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		out := l.dispatch(context.Background(),
			anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(raw)},
			opts, nil, nil, failures, streaks)
		return resultText(out)
	}

	// Two different commands fail the same way: the environment looks wedged,
	// and the directive is correct here.
	run("go vet ./...")
	run("go test ./...")
	ranBefore := len(bash.ran)
	if got := run("go build ./..."); !strings.Contains(got, "environment issue") {
		t.Fatalf("expected the env directive, got %q", got)
	}
	if len(bash.ran) != ranBefore {
		t.Fatal("the directive should refuse the call, not run it")
	}

	// The environment recovers. The very next call must be allowed to run —
	// this is the assertion that fails against a latching breaker.
	bash.err = ""
	if got := run("true"); strings.Contains(got, "environment issue") {
		t.Fatalf("tool stayed latched after the streak was reported: %q", got)
	}
	if len(bash.ran) != ranBefore+1 {
		t.Fatalf("`true` was never executed: ran %v", bash.ran)
	}
}

// Firing once per streak must not cost the anti-loop property: a tool that
// keeps failing has to keep getting told.
func TestShapeStreakRefiresAfterMoreFailures(t *testing.T) {
	proj := t.TempDir()
	reg, bash := testRegistry(t)
	bash.err = "dial tcp: connect: network is unreachable"
	l := New(nil, reg)
	opts := Options{
		WorkingDir: proj,
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
		Approver:   HeadlessApprover(false),
	}
	failures, streaks := map[string]int{}, map[string]errStreak{}
	run := func(cmd string) string {
		raw, _ := json.Marshal(map[string]string{"command": cmd})
		return resultText(l.dispatch(context.Background(),
			anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(raw)},
			opts, nil, nil, failures, streaks))
	}

	run("a")
	run("b")
	if got := run("c"); !strings.Contains(got, "environment issue") {
		t.Fatalf("first directive missing: %q", got)
	}
	// Streak cleared; these two rebuild it, and the third must be told again.
	run("d")
	run("e")
	if got := run("f"); !strings.Contains(got, "environment issue") {
		t.Fatalf("breaker did not re-fire after further failures: %q", got)
	}
}
