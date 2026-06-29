package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

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
	failures := map[string]int{}
	streaks := map[string]errStreak{}
	reveal := func(...string) {}

	textOf := func(b anthropic.BetaContentBlockParamUnion) string {
		if b.OfToolResult == nil || len(b.OfToolResult.Content) == 0 {
			return ""
		}
		return b.OfToolResult.Content[0].OfText.Text
	}

	// First repeatFailureLimit attempts return the normal error and bump the count.
	for i := 0; i < repeatFailureLimit; i++ {
		got := textOf(l.dispatch(context.Background(), tu, Options{}, nil, reveal, failures, streaks))
		if !strings.Contains(got, "No such tool available") {
			t.Fatalf("attempt %d: expected the normal error, got %q", i+1, got)
		}
	}

	// The next identical attempt is short-circuited with the steering message.
	got := textOf(l.dispatch(context.Background(), tu, Options{}, nil, reveal, failures, streaks))
	if !strings.Contains(got, "already tried this exact") {
		t.Fatalf("expected loop-breaker steering, got %q", got)
	}
}

// TestDispatchBreaksSameShapeErrorLoop covers loop-breaker B: same tool,
// DIFFERENT args each call, but the same error shape every time. The
// (name+args) breaker can't detect this — each call has a fresh key — so this
// breaker tracks consecutive same-message failures per tool. Real cases: a
// model retrying Read with new line numbers but the same wrong parameter
// names (line_start/line_end), or a fabricated tool name called repeatedly.
func TestDispatchBreaksSameShapeErrorLoopVariedInputsGetsEnvMessage(t *testing.T) {
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))

	failures := map[string]int{}
	streaks := map[string]errStreak{}
	textOf := func(b anthropic.BetaContentBlockParamUnion) string {
		if b.OfToolResult == nil || len(b.OfToolResult.Content) == 0 {
			return ""
		}
		return b.OfToolResult.Content[0].OfText.Text
	}

	// Three calls to "Find" with DIFFERENT args each time. The error shape
	// stays constant (unknown tool) while the inputs vary — exactly the
	// "model is varying its guesses in good faith but the substrate is
	// wedged" shape we want to flag as environmental, not "stop guessing".
	for i, args := range []map[string]any{{"q": "alpha"}, {"q": "beta"}, {"q": "gamma"}} {
		tu := anthropic.BetaToolUseBlock{ID: "t", Name: "Find", Input: args}
		got := textOf(l.dispatch(context.Background(), tu, Options{}, nil, func(...string) {}, failures, streaks))
		switch i {
		case 0, 1:
			if !strings.Contains(got, "No such tool available: Find") {
				t.Fatalf("call %d: expected the standard error, got %q", i+1, got)
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

func TestDispatchBreaksSameShapeErrorLoopIdenticalInputsKeepsOriginalMessage(t *testing.T) {
	// Same scenario as above but with IDENTICAL inputs — the model really is
	// retrying the same call. The original "stop guessing different values"
	// message is correct here and must not be replaced by the env-flavored one.
	read, _ := tools.NewRead()
	l := New(nil, tools.NewRegistry(read))

	failures := map[string]int{}
	streaks := map[string]errStreak{}
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
		got := textOf(l.dispatch(context.Background(), tu, Options{}, nil, func(...string) {}, failures, streaks))
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
