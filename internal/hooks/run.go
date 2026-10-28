package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
)

// Input is what a hook is told about the thing it is attached to.
//
// The field *names* are Claude Code's, not ones chosen here. Hook scripts are
// the most portable artefact in the whole ecosystem — a few lines of shell
// reading `jq -r .tool_input.file_path` — and a gratuitously different payload
// would mean every existing script needs editing to run under Klaudia for no
// benefit to anyone. Fields Klaudia does not produce are simply absent.
type Input struct {
	Event     Event  `json:"hook_event_name"`
	SessionID string `json:"session_id,omitempty"`
	CWD       string `json:"cwd,omitempty"`
	// ToolName and ToolInput are set for the two tool events. ToolInput is the
	// raw arguments the model produced, verbatim — a hook that wants a path
	// should read it out of here rather than be handed a guess.
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	// ToolResult and ToolError describe a finished call (PostToolUse).
	ToolResult string `json:"tool_response,omitempty"`
	ToolError  bool   `json:"tool_error,omitempty"`
	// Prompt is the text about to be sent (UserPromptSubmit).
	Prompt string `json:"prompt,omitempty"`
	// Message and NotificationType describe why Klaudia wants the user
	// (Notification): a one-line message, and a slug — permission, question,
	// plan, done, error.
	Message          string `json:"message,omitempty"`
	NotificationType string `json:"notification_type,omitempty"`
}

// Result is the combined verdict of every hook that matched an event.
type Result struct {
	// Blocked means the action must not proceed. Reason is fed to the model so
	// it can respond to the refusal; it is never silent.
	Blocked bool
	Reason  string
	// Context is text to add to the conversation: a hook's stdout, or its
	// declared additionalContext.
	Context string
	// Notices are for the user, not the model: hooks that failed to run, bad
	// config, output that had to be truncated. A hook misbehaving is the
	// operator's problem to fix, and telling the model about it only invites it
	// to work around a broken script.
	Notices []string
}

// blockExit is the exit status that means "block this, and here is why".
//
// 2 rather than any non-zero status, because the two outcomes need to be
// distinguishable and only one of them should reach the model. A hook that
// exits 127 because the formatter is not installed has not made a judgement
// about the tool call; reporting that to the model as a refusal would have it
// reason about, and route around, a broken environment. So: 2 is a verdict,
// anything else non-zero is a malfunction. This is Claude Code's convention and
// the reason for keeping it is that hook scripts move between agents.
const blockExit = 2

// maxHookOutput bounds what one hook can put into the conversation.
//
// Hook stdout is injected context, so an unbounded hook is an unbounded prompt:
// `[[hooks]] command = "git log"` in a long-lived repo is megabytes, pasted in
// front of every single user message. 8 KB is generous for the cases the
// feature exists for — a ticket summary, a branch name, a list of changed
// files — and the truncation is reported to the user rather than hidden, since
// a hook that keeps hitting it is misconfigured.
const maxHookOutput = 8 << 10

// output is the optional JSON a hook may print instead of plain text.
//
// Two shapes are accepted. The flat one is Klaudia's, and it is what the docs
// describe. The nested hookSpecificOutput is Claude Code's, accepted for the
// same reason the input field names are theirs: a script that already speaks it
// should not need a translation layer. Neither is required — exit codes and
// plain stdout cover almost everything, and a hook that prints a shopping list
// is treated as having printed a shopping list.
type output struct {
	Decision          string `json:"decision"`
	Reason            string `json:"reason"`
	AdditionalContext string `json:"additionalContext"`

	HookSpecificOutput *struct {
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
		AdditionalContext        string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// blocks reports a refusal declared in JSON, with its reason.
func (o output) blocks() (string, bool) {
	if strings.EqualFold(o.Decision, "block") || strings.EqualFold(o.Decision, "deny") {
		return o.Reason, true
	}
	if h := o.HookSpecificOutput; h != nil {
		if strings.EqualFold(h.PermissionDecision, "deny") || strings.EqualFold(h.PermissionDecision, "block") {
			return h.PermissionDecisionReason, true
		}
	}
	return "", false
}

// context returns the text the hook wants added to the conversation.
func (o output) context() string {
	if o.AdditionalContext != "" {
		return o.AdditionalContext
	}
	if h := o.HookSpecificOutput; h != nil {
		return h.AdditionalContext
	}
	return ""
}

// parseOutput interprets a hook's stdout.
//
// A hook that prints JSON gets the structured reading; anything else is taken
// as plain context. The discrimination is "parses as a JSON object", not "looks
// like it starts with a brace", because the failure mode of guessing wrong in
// the other direction is a hook whose refusal is silently treated as prose.
func parseOutput(stdout string) (output, bool) {
	s := strings.TrimSpace(stdout)
	if !strings.HasPrefix(s, "{") {
		return output{}, false
	}
	var o output
	if err := json.Unmarshal([]byte(s), &o); err != nil {
		return output{}, false
	}
	return o, true
}

// Run executes every hook attached to ev that matches in, in configuration
// order, and combines their verdicts.
//
// Hooks run one at a time, not concurrently. They are allowed to have side
// effects on the working tree — formatting a file is the motivating case — and
// two of them writing the same file at once is a corruption the user cannot
// debug from inside Klaudia. Sequential also makes the combined Reason
// deterministic, which matters because it is the text the model sees.
//
// The first block stops the sequence. A refused tool call is not going to
// happen, so running the rest is work for nothing, and collecting four opinions
// about an action already settled produces a reason nobody can act on.
func (r *Runner) Run(ctx context.Context, in Input, confirm Confirm) Result {
	var res Result
	if r == nil {
		return res
	}
	res.Notices = r.drainNotices()
	if in.SessionID == "" {
		in.SessionID = r.sessionID
	}
	if in.CWD == "" {
		in.CWD = r.cwd
	}
	matched := r.selected(ctx, in, confirm, &res)
	var contexts []string
	for _, h := range matched {
		out, notice, err := r.exec(ctx, h, in)
		if notice != "" {
			res.Notices = append(res.Notices, notice)
		}
		if err != nil {
			// A hook that could not run is reported and skipped. It is not a
			// refusal: see blockExit.
			res.Notices = append(res.Notices, fmt.Sprintf("hook %s failed: %v (from %s)", h.Event, err, h.Source))
			continue
		}
		if out.blocked && h.Event.canBlock() {
			res.Blocked = true
			res.Reason = out.reason(h)
			break
		}
		if out.blocked {
			res.Notices = append(res.Notices, fmt.Sprintf("%s hooks cannot block; %q exited %d", h.Event, h.Command, blockExit))
		}
		if c := strings.TrimSpace(out.context); c != "" {
			contexts = append(contexts, c)
		}
	}
	res.Context = strings.Join(contexts, "\n")
	return res
}

// execResult is one hook's outcome.
type execResult struct {
	blocked bool
	// stderr and declared hold the two places a reason can come from: the
	// stream a shell script writes diagnostics to, and an explicit JSON field.
	stderr   string
	declared string
	context  string
}

// reason is the text the model is told. A hook's own words win; its stderr is
// the fallback, because `echo "generated file" >&2; exit 2` is how a two-line
// shell hook explains itself and demanding JSON for that would make the common
// case the hard one.
func (e execResult) reason(h Hook) string {
	if s := strings.TrimSpace(e.declared); s != "" {
		return s
	}
	if s := strings.TrimSpace(e.stderr); s != "" {
		return s
	}
	return fmt.Sprintf("A %s hook refused this (%s). It gave no reason.", h.Event, h.Command)
}

// exec runs one hook. The error return is reserved for "the hook did not run or
// malfunctioned"; a refusal is a successful run with blocked set.
func (r *Runner) exec(ctx context.Context, h Hook, in Input) (execResult, string, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return execResult{}, "", fmt.Errorf("encoding hook input: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, h.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, sandbox.Shell(), "-c", h.Command)
	cmd.Dir = r.cwd
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Hooks are not sandboxed, whatever [sandbox] says. That setting is about
	// confining the model's commands; a hook is the user's own automation,
	// approved by them, and the things it exists to do — run the project's
	// formatter, touch a git index, post a notification — are exactly what
	// confinement forbids. A hook running under sandbox-exec would fail in ways
	// whose cause is invisible from the config file that declared it.
	cmd.Env = append(os.Environ(),
		"KLAUDIA_PROJECT_DIR="+r.cwd,
		"KLAUDIA_HOOK_EVENT="+string(in.Event),
	)
	// Own process group, cancelled as a group, so a hook that backgrounds
	// something does not outlive its timeout. Same reason as the Bash tool.
	sandbox.ProcGroup(cmd)

	runErr := cmd.Run()
	var res execResult
	res.stderr = stderr.String()
	outText, notice := clamp(stdout.String(), h)

	if o, ok := parseOutput(outText); ok {
		if reason, blocked := o.blocks(); blocked {
			res.blocked, res.declared = true, reason
		}
		res.context = o.context()
	} else {
		res.context = outText
	}

	switch {
	case runErr == nil:
		return res, notice, nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return execResult{}, notice, fmt.Errorf("timed out after %s", h.Timeout)
	case ctx.Err() != nil:
		// The session was cancelled, not the hook. Not a malfunction, and not
		// worth a notice telling the user about a consequence of their own Esc.
		return execResult{}, notice, nil
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) && ee.ExitCode() == blockExit {
		res.blocked = true
		return res, notice, nil
	}
	// Any other non-zero status: a malfunction. Its stderr goes to the user.
	msg := strings.TrimSpace(firstLine(res.stderr))
	if msg == "" {
		msg = runErr.Error()
	} else {
		msg = fmt.Sprintf("%v: %s", runErr, msg)
	}
	return execResult{}, notice, errors.New(msg)
}

// clamp bounds a hook's stdout, returning a notice when it had to cut.
func clamp(s string, h Hook) (string, string) {
	if len(s) <= maxHookOutput {
		return s, ""
	}
	return s[:maxHookOutput], fmt.Sprintf(
		"hook output truncated to %d bytes (%s produced %d); it is injected into the conversation, so keep it small",
		maxHookOutput, h.Command, len(s))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
