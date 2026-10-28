package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greenthread-ai/klaudia/internal/config"
)

// confirm approves whatever it is shown, so a test about the execution protocol
// is not also a test about the trust prompt. The prompt's own behaviour — when
// it is raised, what an answer buys and how long it lasts — is trust_test.go.
var confirm Confirm = func(context.Context, []Hook, string) bool { return true }

func runner(t *testing.T, user, project []config.Hook) *Runner {
	t.Helper()
	r := New(t.TempDir(), "sess", user, project)
	r.Store = Store{Path: filepath.Join(t.TempDir(), "hooks.json")}
	return r
}

func TestCompileReportsBadEntries(t *testing.T) {
	tests := []struct {
		name    string
		entry   config.Hook
		kept    bool
		wantErr string
	}{
		{
			name:  "a well formed tool hook",
			entry: config.Hook{Event: "PreToolUse", Matcher: "Edit|Write", Command: "true"},
			kept:  true,
		},
		{
			name:    "an event that does not exist",
			entry:   config.Hook{Event: "SessionStarted", Command: "true"},
			wantErr: "unknown event",
		},
		{
			name:    "no command to run",
			entry:   config.Hook{Event: "PreToolUse", Command: "   "},
			wantErr: "has no command",
		},
		{
			name:    "a matcher that is not a regexp",
			entry:   config.Hook{Event: "PreToolUse", Matcher: "Edit(", Command: "true"},
			wantErr: "bad matcher",
		},
		{
			name:    "a timeout that is not a duration",
			entry:   config.Hook{Event: "PreToolUse", Command: "true", Timeout: "soon"},
			wantErr: "bad timeout",
		},
		{
			name:    "a timeout that is not positive",
			entry:   config.Hook{Event: "PreToolUse", Command: "true", Timeout: "0s"},
			wantErr: "not positive",
		},
		{
			// Kept, because the hook is runnable and dropping it would be a
			// surprise; reported, because it does not do what it looks like.
			name:    "a matcher on an event with no tool",
			entry:   config.Hook{Event: "SessionStart", Matcher: "Edit", Command: "true"},
			kept:    true,
			wantErr: "no tool to match",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, errs := compile([]config.Hook{tc.entry}, "cfg", false)
			if len(got) != boolToInt(tc.kept) {
				t.Fatalf("kept %d hooks, want %d", len(got), boolToInt(tc.kept))
			}
			joined := joinErrs(errs)
			if tc.wantErr == "" {
				if joined != "" {
					t.Fatalf("unexpected problem: %s", joined)
				}
				return
			}
			if !strings.Contains(joined, tc.wantErr) {
				t.Fatalf("problem %q does not mention %q", joined, tc.wantErr)
			}
		})
	}
}

func TestCompileDefaultsAndOverridesTimeout(t *testing.T) {
	got, errs := compile([]config.Hook{
		{Event: "PreToolUse", Command: "a"},
		{Event: "PreToolUse", Command: "b", Timeout: "90s"},
	}, "cfg", false)
	if joined := joinErrs(errs); joined != "" {
		t.Fatalf("unexpected problem: %s", joined)
	}
	if got[0].Timeout != defaultTimeout {
		t.Errorf("hook with no timeout got %s, want the %s default", got[0].Timeout, defaultTimeout)
	}
	if got[1].Timeout != 90*time.Second {
		t.Errorf("hook timeout = %s, want 90s", got[1].Timeout)
	}
}

func TestMatcherSelectsByToolName(t *testing.T) {
	tests := []struct {
		name    string
		matcher string
		tool    string
		want    bool
	}{
		{name: "an empty matcher takes every tool", matcher: "", tool: "Bash", want: true},
		{name: "an alternation takes either", matcher: "Edit|Write", tool: "Write", want: true},
		{name: "a non-match is skipped", matcher: "Edit|Write", tool: "Read", want: false},
		{name: "an anchored matcher is honoured", matcher: "^Edit$", tool: "NotebookEdit", want: false},
		{name: "an unanchored matcher is a substring", matcher: "Edit", tool: "NotebookEdit", want: true},
		{name: "an MCP name can be targeted", matcher: "^mcp__github__", tool: "mcp__github__list_issues", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hs, errs := compile([]config.Hook{{Event: "PreToolUse", Matcher: tc.matcher, Command: "true"}}, "cfg", false)
			if joined := joinErrs(errs); joined != "" {
				t.Fatalf("unexpected problem: %s", joined)
			}
			if got := hs[0].matches(tc.tool); got != tc.want {
				t.Errorf("matches(%q) = %v, want %v", tc.tool, got, tc.want)
			}
		})
	}
}

// The protocol is the contract hook authors write against, so it is tested
// through real processes rather than a fake: the exit status, the two output
// streams and the JSON reading all have to agree with what a shell actually
// produces.
func TestHookProtocol(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		wantBlocked bool
		wantReason  string
		wantContext string
		wantNotice  string
	}{
		{
			name:        "exit 0 with stdout adds context",
			command:     `echo "ticket PROJ-12"`,
			wantContext: "ticket PROJ-12",
		},
		{
			name:        "exit 2 blocks and stderr is the reason",
			command:     `echo "generated file, edit the template" >&2; exit 2`,
			wantBlocked: true,
			wantReason:  "generated file, edit the template",
		},
		{
			name:        "a flat JSON decision blocks with its reason",
			command:     `echo '{"decision":"block","reason":"vendored"}'`,
			wantBlocked: true,
			wantReason:  "vendored",
		},
		{
			// Claude Code's shape. Accepted so an existing hook script ports
			// without edits.
			name:        "a nested permissionDecision blocks with its reason",
			command:     `echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"protected path"}}'`,
			wantBlocked: true,
			wantReason:  "protected path",
		},
		{
			name:        "additionalContext is preferred over raw stdout",
			command:     `echo '{"additionalContext":"on branch main"}'`,
			wantContext: "on branch main",
		},
		{
			name:        "stdout that is not JSON is taken as prose",
			command:     `echo 'not { json'`,
			wantContext: "not { json",
		},
		{
			// The distinction the whole exit-code convention exists for: a
			// missing formatter has made no judgement about the tool call.
			name:       "any other non-zero exit is a malfunction, not a refusal",
			command:    `echo "formatter: not found" >&2; exit 127`,
			wantNotice: "formatter: not found",
		},
		{
			name:        "a blocking exit may also state its reason in JSON",
			command:     `echo '{"reason":"too risky"}'; exit 2`,
			wantBlocked: true,
			wantReason:  "too risky",
		},
		{
			name:        "a refusal with nothing to say still says something",
			command:     `exit 2`,
			wantBlocked: true,
			wantReason:  "gave no reason",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := runner(t, []config.Hook{{Event: "PreToolUse", Command: tc.command}}, nil)
			res := r.Run(context.Background(), Input{Event: PreToolUse, ToolName: "Edit"}, confirm)

			if res.Blocked != tc.wantBlocked {
				t.Fatalf("Blocked = %v, want %v (reason %q, notices %v)", res.Blocked, tc.wantBlocked, res.Reason, res.Notices)
			}
			if tc.wantReason != "" && !strings.Contains(res.Reason, tc.wantReason) {
				t.Errorf("Reason = %q, want it to mention %q", res.Reason, tc.wantReason)
			}
			if got := strings.TrimSpace(res.Context); got != tc.wantContext {
				t.Errorf("Context = %q, want %q", got, tc.wantContext)
			}
			notices := strings.Join(res.Notices, "\n")
			if tc.wantNotice != "" && !strings.Contains(notices, tc.wantNotice) {
				t.Errorf("notices = %q, want them to mention %q", notices, tc.wantNotice)
			}
			if tc.wantNotice == "" && notices != "" {
				t.Errorf("unexpected notices: %q", notices)
			}
		})
	}
}

// A malfunctioning hook must not reach the model: it would reason about, and
// route around, a broken environment rather than its own actions.
func TestMalfunctionIsNotReportedToTheModel(t *testing.T) {
	r := runner(t, []config.Hook{{Event: "PreToolUse", Command: `exit 127`}}, nil)
	res := r.Run(context.Background(), Input{Event: PreToolUse, ToolName: "Edit"}, confirm)
	if res.Blocked {
		t.Error("a hook that could not run blocked the call")
	}
	if res.Reason != "" || res.Context != "" {
		t.Errorf("a malfunction reached the model: reason %q context %q", res.Reason, res.Context)
	}
	if len(res.Notices) == 0 {
		t.Error("a malfunction was not reported to the user either")
	}
}

func TestHookReceivesTheCallOnStdin(t *testing.T) {
	// The payload is written to a file rather than echoed back as context: it
	// is itself a JSON object, so stdout would be read as a protocol response
	// and discarded. Asserting on the file is asserting on the real encoding
	// the hook was handed.
	payload := filepath.Join(t.TempDir(), "payload.json")
	r := runner(t, []config.Hook{{Event: "PreToolUse", Command: `cat > ` + payload}}, nil)
	r.Run(context.Background(), Input{
		Event:     PreToolUse,
		SessionID: "sess-1",
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"/tmp/x.go"}`),
	}, confirm)
	var got Input
	raw := readFile(t, payload)
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("hook stdin was not the Input JSON: %v (%q)", err, raw)
	}
	if got.Event != PreToolUse || got.ToolName != "Edit" || got.SessionID != "sess-1" {
		t.Errorf("payload = %+v, want the event, tool and session", got)
	}
	if string(got.ToolInput) != `{"file_path":"/tmp/x.go"}` {
		t.Errorf("tool_input = %s, want the arguments verbatim", got.ToolInput)
	}
}

func TestTimeoutIsReportedAndDoesNotBlock(t *testing.T) {
	r := runner(t, []config.Hook{{Event: "PreToolUse", Command: `sleep 5`, Timeout: "40ms"}}, nil)
	start := time.Now()
	res := r.Run(context.Background(), Input{Event: PreToolUse, ToolName: "Edit"}, confirm)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the timeout did not fire: took %s", elapsed)
	}
	if res.Blocked {
		t.Error("a hook that timed out blocked the call")
	}
	if !strings.Contains(strings.Join(res.Notices, "\n"), "timed out") {
		t.Errorf("notices = %v, want one mentioning the timeout", res.Notices)
	}
}

func TestOutputIsClampedWithANotice(t *testing.T) {
	r := runner(t, []config.Hook{{Event: "UserPromptSubmit", Command: `yes xxxxxxxx | head -c 20000`}}, nil)
	res := r.Run(context.Background(), Input{Event: UserPromptSubmit, Prompt: "hi"}, confirm)
	if len(res.Context) > maxHookOutput {
		t.Errorf("context is %d bytes, want it clamped to %d", len(res.Context), maxHookOutput)
	}
	if !strings.Contains(strings.Join(res.Notices, "\n"), "truncated") {
		t.Errorf("notices = %v, want one mentioning truncation", res.Notices)
	}
}

func TestHooksRunInOrderAndStopAtTheFirstBlock(t *testing.T) {
	log := filepath.Join(t.TempDir(), "order")
	r := runner(t, []config.Hook{
		{Event: "PreToolUse", Command: `echo first >> ` + log},
		{Event: "PreToolUse", Command: `echo second >> ` + log + `; exit 2`},
		{Event: "PreToolUse", Command: `echo third >> ` + log},
	}, nil)
	res := r.Run(context.Background(), Input{Event: PreToolUse, ToolName: "Edit"}, confirm)
	if !res.Blocked {
		t.Fatal("want the call blocked")
	}
	if got := readFile(t, log); got != "first\nsecond\n" {
		t.Errorf("ran %q, want first and second only — a settled refusal should not keep running hooks", got)
	}
}

func TestEveryHooksContextIsCollected(t *testing.T) {
	r := runner(t, []config.Hook{
		{Event: "UserPromptSubmit", Command: `echo one`},
		{Event: "UserPromptSubmit", Command: `echo two`},
	}, nil)
	res := r.Run(context.Background(), Input{Event: UserPromptSubmit, Prompt: "hi"}, confirm)
	if res.Context != "one\ntwo" {
		t.Errorf("Context = %q, want both hooks' output in order", res.Context)
	}
}

// SessionStart has nothing to block: the session is already starting because
// the user asked for it.
func TestSessionStartCannotBlock(t *testing.T) {
	r := runner(t, []config.Hook{{Event: "SessionStart", Command: `echo nope >&2; exit 2`}}, nil)
	res := r.Run(context.Background(), Input{Event: SessionStart}, confirm)
	if res.Blocked {
		t.Error("a SessionStart hook blocked the session")
	}
	if !strings.Contains(strings.Join(res.Notices, "\n"), "cannot block") {
		t.Errorf("notices = %v, want one saying the event cannot block", res.Notices)
	}
}

func TestOnlyMatchingEventsRun(t *testing.T) {
	log := filepath.Join(t.TempDir(), "fired")
	r := runner(t, []config.Hook{
		{Event: "PreToolUse", Command: `echo pre >> ` + log},
		{Event: "PostToolUse", Command: `echo post >> ` + log},
	}, nil)
	r.Run(context.Background(), Input{Event: PostToolUse, ToolName: "Edit"}, confirm)
	if got := readFile(t, log); got != "post\n" {
		t.Errorf("fired %q, want only the PostToolUse hook", got)
	}
}

func TestHasSkipsEventsWithNoHooks(t *testing.T) {
	r := runner(t, []config.Hook{{Event: "PostToolUse", Command: "true"}}, nil)
	if r.Has(PreToolUse) {
		t.Error("Has(PreToolUse) is true with only a PostToolUse hook configured")
	}
	if !r.Has(PostToolUse) {
		t.Error("Has(PostToolUse) is false with one configured")
	}
	var nilRunner *Runner
	if nilRunner.Has(PreToolUse) {
		t.Error("a nil Runner claims to have hooks")
	}
	if res := nilRunner.Run(context.Background(), Input{Event: PreToolUse}, confirm); res.Blocked {
		t.Error("a nil Runner blocked a call")
	}
}

func TestConfigProblemsAreReportedOnce(t *testing.T) {
	r := runner(t, []config.Hook{
		{Event: "Nonsense", Command: "true"},
		{Event: "PreToolUse", Command: "true"},
	}, nil)
	first := r.Run(context.Background(), Input{Event: PreToolUse, ToolName: "Edit"}, confirm)
	if !strings.Contains(strings.Join(first.Notices, "\n"), "unknown event") {
		t.Fatalf("notices = %v, want the config problem", first.Notices)
	}
	second := r.Run(context.Background(), Input{Event: PreToolUse, ToolName: "Edit"}, confirm)
	if len(second.Notices) != 0 {
		t.Errorf("notices repeated on the next call: %v", second.Notices)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func joinErrs(errs []error) string {
	parts := make([]string, len(errs))
	for i, err := range errs {
		parts[i] = err.Error()
	}
	return strings.Join(parts, "\n")
}

// A Notification hook gets the reason and the message, in Claude Code's
// field names, and cannot block anything: there is nothing to block.
func TestNotificationHook(t *testing.T) {
	out := filepath.Join(t.TempDir(), "payload.json")
	r := runner(t, []config.Hook{{Event: "Notification", Command: "cat > " + out + "; exit 2"}}, nil)
	if !r.Has(Notification) {
		t.Fatal("a Notification hook was not compiled")
	}
	res := r.Run(context.Background(), Input{Event: Notification, Message: "Klaudia has a question", NotificationType: "question"}, confirm)
	if res.Blocked {
		t.Error("a Notification hook's exit 2 blocked something")
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"hook_event_name":"Notification"`, `"message":"Klaudia has a question"`, `"notification_type":"question"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("payload lacks %s: %s", want, b)
		}
	}
}
