package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Hooks are wired in at four points and nowhere else. This file holds the three
// decisions shared by all of them, so each call site is a line rather than a
// policy.
//
// Where they sit relative to the gate is the important one. A PreToolUse hook
// runs *after* the host gate and the permission check have both allowed the
// call, which means a hook can stop something Klaudia was willing to do and can
// never permit something it was not. A config file that could grant permission
// would be a way to disable the gate by writing a file, and a project config
// is a file that arrives with a clone.

// Hooks is a source of lifecycle hooks: shell commands a user or project wants
// run at fixed points of a turn. Klaudia's is hooks.Runner, adapted in the CLI
// (cli/hooks.go) so the loop does not depend on how hooks are configured or
// trusted. Nil — the common case — costs nothing.
type Hooks interface {
	// Has reports whether any hook is attached to ev, so the two per-call
	// events cost nothing when none are.
	Has(ev HookEvent) bool
	// ClaimSessionStart reports, once per session, that SessionStart should
	// fire now.
	ClaimSessionStart() bool
	// Run runs ev's hooks. Hooks that need the user's agreement first are put
	// to them through approver; nil means there is no one to ask, which the
	// source should distinguish from a refusal.
	Run(ctx context.Context, in HookInput, approver Approver) HookResult
}

// HookEvent names a lifecycle point.
type HookEvent string

// The four points the loop fires hooks at.
const (
	HookSessionStart     HookEvent = "SessionStart"
	HookUserPromptSubmit HookEvent = "UserPromptSubmit"
	HookPreToolUse       HookEvent = "PreToolUse"
	HookPostToolUse      HookEvent = "PostToolUse"
)

// HookInput is what a hook is told about the moment it runs at.
type HookInput struct {
	Event      HookEvent
	CWD        string
	ToolName   string
	ToolInput  json.RawMessage
	ToolResult string
	ToolError  bool
	Prompt     string
}

// HookResult is what the hooks for one event decided and said.
type HookResult struct {
	Blocked bool     // a PreToolUse hook refused the call
	Reason  string   // why, for the model
	Context string   // text to inject (SessionStart, UserPromptSubmit) or feedback (PostToolUse)
	Notices []string // for the user, never the model
}

// fireHooks runs the hooks attached to an event and surfaces anything the user
// needs to know about them.
//
// Notices go to the user as notice events and never to the model. A hook that
// failed to start, a typo in the config, output too big to inject: these are the
// operator's to fix, and telling the model its environment is misconfigured only
// invites it to work around the misconfiguration.
func fireHooks(ctx context.Context, opts Options, emit Emitter, in HookInput) HookResult {
	if opts.Hooks == nil {
		return HookResult{}
	}
	in.CWD = opts.WorkingDir
	res := opts.Hooks.Run(ctx, in, opts.Approver)
	for _, n := range res.Notices {
		if emit != nil {
			emit(Event{Type: "notice", Content: "hook: " + n})
		}
	}
	return res
}

// hookEnabled reports whether it is worth building an Input for this event. The
// two tool events fire on every single tool call, so the common case — no hooks
// configured, or none for this event — must not cost a marshal and a result
// collapse per call.
func hookEnabled(opts Options, ev HookEvent) bool {
	return opts.Hooks != nil && opts.Hooks.Has(ev)
}

// hookBlockedMsg is what the model is told when a hook refuses a tool call.
//
// It names the hook as the source. Without that the model sees a refusal in the
// tool's own voice and concludes the tool is broken — then retries it, which is
// the loop the breakers exist to stop. "Something outside me said no, for this
// reason" is a thing it can act on.
func hookBlockedMsg(tool, reason string) string {
	if strings.TrimSpace(reason) == "" {
		reason = "no reason given"
	}
	return fmt.Sprintf("A PreToolUse hook blocked this %s call: %s", tool, reason)
}

// hookFeedback appends a PostToolUse hook's output to a tool result.
//
// Labelled, and after the result rather than before it. The model has to be able
// to tell the tool's own output from a hook's commentary — a formatter saying
// "reformatted 1 file" is not something Write printed — and appending keeps the
// result's own head intact for a tool whose first line is the answer.
func hookFeedback(content, feedback string) string {
	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return content
	}
	note := "[PostToolUse hook]\n" + feedback
	if content == "" {
		return note
	}
	return content + "\n\n" + note
}

// appendHookContext collects a hook result's context, dropping the empty case.
func appendHookContext(into []string, res HookResult) []string {
	if c := strings.TrimSpace(res.Context); c != "" {
		return append(into, c)
	}
	return into
}

// promptBlocks builds the user message: injected context first, as its own text
// block, then the prompt.
//
// A separate block rather than one concatenated string, and the context first.
// Separate because a frontend replaying the transcript can then tell what the
// user typed from what a hook added — glue them together and the user's own
// message in the history is not what they wrote. First because the prompt is the
// instruction and context that follows an instruction reads as part of it: a
// hook that pastes a file listing after "delete the stale ones" has changed what
// the sentence means.
func promptBlocks(prompt string, injected []string) []anthropic.BetaContentBlockParamUnion {
	var blocks []anthropic.BetaContentBlockParamUnion
	for _, c := range injected {
		blocks = append(blocks, anthropic.NewBetaTextBlock("<hook-context>\n"+c+"\n</hook-context>"))
	}
	if prompt != "" {
		blocks = append(blocks, anthropic.NewBetaTextBlock(prompt))
	}
	return blocks
}

// toolInputFor gives a hook the model's arguments exactly as they arrived. A
// hook that wants a file path reads tool_input.file_path; handing it a parsed
// guess would make Klaudia's idea of the call authoritative over the call.
func toolInputFor(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return json.RawMessage(raw)
}
