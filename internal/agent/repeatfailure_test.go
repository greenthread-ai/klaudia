package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/tools"
)

func TestRepeatedFailureMsg(t *testing.T) {
	edit := repeatedFailureMsg("Edit", 3)
	if !strings.Contains(edit, "already tried this exact Edit call 3 times") {
		t.Errorf("Edit message missing the count: %q", edit)
	}
	if !strings.Contains(edit, "Use Read") {
		t.Errorf("Edit message should steer to Read: %q", edit)
	}
	// A tool without specific guidance still gets the generic stop-repeating push.
	if got := repeatedFailureMsg("Bash", 2); !strings.Contains(got, "Stop repeating it") {
		t.Errorf("generic message = %q", got)
	}
}

// TestDispatchBreaksRetryLoop drives dispatch with the same failing call
// repeatedly: once the per-Run failure count reaches the limit, dispatch must
// stop and return the directive steering instead of the original error.
func TestDispatchBreaksRetryLoop(t *testing.T) {
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))

	// An unknown tool always fails (errResult), which increments the counter.
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "Frobnicate", Input: map[string]any{"a": 1}}
	fs := newFailureState()
	reveal := func(...string) {}

	textOf := func(b anthropic.BetaContentBlockParamUnion) string {
		if b.OfToolResult == nil || len(b.OfToolResult.Content) == 0 {
			return ""
		}
		return b.OfToolResult.Content[0].OfText.Text
	}

	// First repeatFailureLimit attempts return the normal error and bump the count.
	for i := 0; i < repeatFailureLimit; i++ {
		got := textOf(l.dispatch(context.Background(), tu, Options{}, nil, reveal, fs))
		if !strings.Contains(got, "No such tool available") {
			t.Fatalf("attempt %d: expected the normal error, got %q", i+1, got)
		}
	}

	// The next identical attempt is short-circuited with the steering message.
	got := textOf(l.dispatch(context.Background(), tu, Options{}, nil, reveal, fs))
	if !strings.Contains(got, "already tried this exact") {
		t.Fatalf("expected loop-breaker steering, got %q", got)
	}
}

// TestHostRefusalDoesNotLatchTheTool covers the interaction that made a whole
// session's Bash unusable. The host gate refuses with the same text whatever
// the command was, so two refused commands looked to loop-breaker B like one
// error shape across different inputs — the environment-is-wedged signature.
// B then refuses before the tool executes, and the only thing that clears a
// streak is a successful execution, so the tool stayed dead for the rest of
// the Run: every later call, including read-only ones, came back as "shell
// wedged". A refusal is a decision about one command, not evidence the tool is
// broken, and must not count.
func TestHostRefusalDoesNotLatchTheTool(t *testing.T) {
	reg, bash := testRegistry(t)
	l := New(nil, reg)

	fs := newFailureState()
	opts := Options{
		WorkingDir: t.TempDir(),
		// The real gate refuses with the same text whatever the command was;
		// this one does exactly that, which is the shape that latched.
		Gate:       refusingGate{match: "sudo", msg: "Changes this machine. Take another route if one exists."},
		Permission: permission.Context{Mode: permission.StaticMode(permission.ModeAutonomous)},
	}

	// Two DIFFERENT host-changing commands. Both are stopped at the gate.
	for _, cmd := range []string{
		"sudo systemctl restart nginx",
		"sudo launchctl stop com.example.agent",
	} {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(bashInput(cmd))}
		l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs)
	}
	if len(bash.ran) != 0 {
		t.Fatalf("a host-changing command ran anyway: %v", bash.ran)
	}
	if s, ok := fs.streaks["Bash"]; ok {
		t.Fatalf("gate refusals fed the same-shape streak: %+v", s)
	}

	// The tool must still work. Before the fix this call never reached the
	// registry — loop-breaker B answered it with a quote of the stale refusal.
	tu := anthropic.BetaToolUseBlock{ID: "t3", Name: "Bash", Input: json.RawMessage(bashInput("git status --short"))}
	body := resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs))
	if len(bash.ran) != 1 || bash.ran[0] != "git status --short" {
		t.Fatalf("the tool latched: benign command never ran (ran=%v, result=%q)", bash.ran, body)
	}
}

// Loop-breaker A refuses to re-run an identical failing call. That refusal is
// ours, not the tool's, so it must not be recorded as another failure — doing
// so let A feed B until B latched the tool for every input.
func TestLoopBreakerARefusalDoesNotFeedB(t *testing.T) {
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))

	fs := newFailureState()
	tu := anthropic.BetaToolUseBlock{ID: "t1", Name: "Frobnicate", Input: map[string]any{"a": 1}}

	// Fail it to the limit, then keep hammering the identical call. Each of
	// those later calls is answered by A.
	for i := 0; i < repeatFailureLimit+3; i++ {
		l.dispatch(context.Background(), tu, Options{}, nil, func(...string) {}, fs)
	}

	// The streak may hold the genuine failures from before A engaged, but it
	// must not have grown past the limit on the back of A's own refusals.
	if st := fs.streaks["Frobnicate"]; st.count > repeatFailureLimit {
		t.Fatalf("breaker A's refusals fed breaker B: streak=%+v", st)
	}
	got := resultText(l.dispatch(context.Background(), tu, Options{}, nil, func(...string) {}, fs))
	if !strings.Contains(got, "already tried this exact") {
		t.Fatalf("expected breaker A to keep steering the identical call, got %q", got)
	}
}

// TestDispatchBreaksSameShapeErrorLoop covers loop-breaker B: same tool,
// DIFFERENT args each call, but the same error shape every time. The
// (name+args) breaker can't detect this — each call has a fresh key — so this
// breaker tracks consecutive same-message failures per tool.
//
// Here the tool RUNS and fails identically whatever it is asked to do, which is
// what a wedged shell or an unreachable network looks like from dispatch.
func TestDispatchBreaksSameShapeErrorLoopVariedInputsGetsEnvMessage(t *testing.T) {
	reg, bash := testRegistry(t)
	bash.err = "fork/exec /bin/zsh: resource temporarily unavailable"
	l := New(nil, reg)

	fs := newFailureState()
	opts := Options{Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)}}

	// Three DIFFERENT commands, same execution failure each time.
	for i, cmd := range []string{"ls", "pwd", "echo hello"} {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Bash", Input: json.RawMessage(bashInput(cmd))}
		got := resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs))
		switch i {
		case 0, 1:
			if !strings.Contains(got, "resource temporarily unavailable") {
				t.Fatalf("call %d: expected the tool's own error, got %q", i+1, got)
			}
		case 2:
			// Env-flavored message: acknowledges varied inputs, suggests
			// recovery moves rather than "guess differently".
			if !strings.Contains(got, "across DIFFERENT inputs") {
				t.Fatalf("call %d: expected env-flavored message, got %q", i+1, got)
			}
			if !strings.Contains(got, "environment issue") || !strings.Contains(got, "recurring error") {
				t.Fatalf("call %d: env message should mention the env hypothesis and quote the error, got %q", i+1, got)
			}
			// Must NOT use the "stop guessing values" framing — that's only
			// appropriate when the input was identical across failures.
			if strings.Contains(got, "guessing different values") {
				t.Fatalf("call %d: env message should not blame the model for guessing, got %q", i+1, got)
			}
		}
	}
}

// A failure that happens BEFORE the tool runs is never evidence about the
// environment, however many times it repeats and however much the inputs vary.
// The env directive tells the model to reset state, restart servers or give up
// on the tool; aimed at a rejected input it sends the model to fix a substrate
// that was never involved, instead of reading the error that names the problem.
//
// Real case: AskUserQuestion called with a stray extra property, rejected by
// the schema four times with character-identical text, and answered with "This
// looks like an environment issue (shell wedged, …)".
func TestPreExecFailuresNeverGetEnvMessage(t *testing.T) {
	read, _ := tools.NewRead()
	ask, _ := tools.NewAskUserQuestion()
	l := New(nil, tools.NewRegistry(read, ask))
	opts := Options{Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)}}

	cases := []struct {
		name   string
		tool   string
		inputs []map[string]any
		want   string
	}{
		{
			name: "unknown tool name",
			tool: "Find",
			inputs: []map[string]any{
				{"q": "alpha"}, {"q": "beta"}, {"q": "gamma"},
			},
			want: "No such tool available",
		},
		{
			name: "input that does not validate",
			tool: "Read",
			inputs: []map[string]any{
				{"line_start": 1, "line_end": 20},
				{"line_start": 21, "line_end": 40},
				{"line_start": 41, "line_end": 60},
			},
			want: "accepts:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFailureState()
			var got string
			for _, args := range tc.inputs {
				tu := anthropic.BetaToolUseBlock{ID: "t", Name: tc.tool, Input: args}
				got = resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs))
			}
			if strings.Contains(got, "environment issue") || strings.Contains(got, "across DIFFERENT inputs") {
				t.Fatalf("pre-execution failure blamed on the environment: %q", got)
			}
			// It still has to break the loop — just with the right diagnosis,
			// and quoting the error the model needs to read.
			if !strings.Contains(got, "SAME error") || !strings.Contains(got, tc.want) {
				t.Fatalf("expected the same-shape directive quoting the real error, got %q", got)
			}
		})
	}
}

func TestDispatchBreaksSameShapeErrorLoopIdenticalInputsKeepsOriginalMessage(t *testing.T) {
	// Same scenario as above but with IDENTICAL inputs — the model really is
	// retrying the same call. The original "stop guessing different values"
	// message is correct here and must not be replaced by the env-flavored one.
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))

	fs := newFailureState()
	textOf := func(b anthropic.BetaContentBlockParamUnion) string {
		if b.OfToolResult == nil || len(b.OfToolResult.Content) == 0 {
			return ""
		}
		return b.OfToolResult.Content[0].OfText.Text
	}

	// Loop-breaker A fires first when the (name+args) key matches twice — it
	// uses repeatedFailureMsg, NOT repeatedShapeFailureMsg. To exercise
	// loop-breaker B with identical-input semantics we need calls whose
	// JSON-encoded input is the same but whose tool-name lookup hits a
	// different shape... the simplest harness is calls with the SAME
	// unknown name and SAME args, knowing that A fires first. That's fine:
	// the assertion is that A's message is what surfaces, not B's env one.
	args := map[string]any{"q": "alpha"}
	for i := range 3 {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Find", Input: args}
		got := textOf(l.dispatch(context.Background(), tu, Options{}, nil, func(...string) {}, fs))
		switch i {
		case 0, 1:
			if !strings.Contains(got, "No such tool available: Find") {
				t.Fatalf("call %d: expected the standard error, got %q", i+1, got)
			}
		case 2:
			// Loop-breaker A catches identical-input retries first. Its
			// directive is the right one for this scenario — the env-flavored
			// message must not fire here.
			if strings.Contains(got, "across DIFFERENT inputs") {
				t.Fatalf("call %d: identical inputs must not trigger env message, got %q", i+1, got)
			}
		}
	}
}

// Loop-breaker B must not answer the call that FIXES the problem. A streak
// built from identical inputs is evidence about that one call; a call with a
// different input has not failed yet and gets to run.
//
// Real case: two Reads of "~/Desktop/Screenshot 2026-10-21 at 4.44.59 pm.png"
// failed identically (the real filename holds U+202F where the model typed a
// space), the model worked that out, copied the file to /tmp/wf.png and read
// that instead — and got back the recorded error about the Desktop path, which
// reads as "the file you just made does not exist".
func TestBreakerBLetsACorrectedCallThrough(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "wf.txt")
	if err := os.WriteFile(good, []byte("it worked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))
	opts := Options{Permission: permission.Context{Mode: permission.StaticMode(permission.ModeBypassPermissions)}}
	fs := newFailureState()

	bad, _ := json.Marshal(map[string]any{"file_path": filepath.Join(dir, "nope.txt")})
	for i := 0; i < repeatFailureLimit; i++ {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Read", Input: json.RawMessage(bad)}
		if got := resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs)); !strings.Contains(got, "does not exist") {
			t.Fatalf("attempt %d: want the real error, got %q", i+1, got)
		}
	}

	in, _ := json.Marshal(map[string]any{"file_path": good})
	tu := anthropic.BetaToolUseBlock{ID: "t3", Name: "Read", Input: json.RawMessage(in)}
	got := resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs))
	if !strings.Contains(got, "it worked") {
		t.Fatalf("the corrected call was refused: %q", got)
	}

	// The identical call is still refused — breaker A, unchanged.
	tu = anthropic.BetaToolUseBlock{ID: "t4", Name: "Read", Input: json.RawMessage(bad)}
	if got := resultText(l.dispatch(context.Background(), tu, opts, nil, func(...string) {}, fs)); !strings.Contains(got, "already tried this exact") {
		t.Fatalf("identical retry should still be stopped, got %q", got)
	}
}
