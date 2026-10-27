package agent

import (
	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

// The permission mode was invisible to the model, and the model is the one
// working under it.
//
// It found out by being refused. In plan mode it would reason its way to an
// edit, call Edit, and get "plan mode is read-only; mutations are not allowed"
// — a wasted turn, and worse, a plan built without knowing which checks were
// available to it. Leaving plan mode was the mirror image: the user was told,
// the transcript recorded it, and the model carried on hedging about work it
// was now free to do.
//
// So the mode goes in the system prompt, per turn, and a change is announced as
// an event. The loop is the only place that sees the live mode function, which
// means every route that changes it — the picker, /mode, /plan, approving a
// plan, a stream-json control request — is covered without any of them knowing
// about this.

// modeClause describes the active mode to the model. Autonomous says nothing:
// it is the mode the base prompt already assumes, and a clause restating the
// default on every turn is tokens spent to change nothing.
func modeClause(mode permission.Mode) string {
	switch mode {
	case permission.ModePlan:
		// Naming the available commands is not padding. The clause used to say
		// "commands with effects" are refused, which was both wrong (every
		// command was) and useless as guidance: a model told that executing is
		// risky stops executing, so it never discovered that `git log` was
		// always going to be allowed. The plan that came out was written
		// without having looked at the branch it was planning against.
		return "\n\n# Permission mode: plan\n" +
			"You are in plan mode. Nothing you do may change a file: edits, writes and MCP " +
			"calls are refused, and so is any command that would write — including builds and " +
			"test runs, which can leave output behind. Commands that only inspect are " +
			"available and you are expected to use them: `git log`, `git diff`, `git status`, " +
			"`ls`, `sed -n`, `go doc`, `go list`. A command nothing can classify will ask the user " +
			"rather than be refused. Research the request properly, then present the plan with " +
			"ExitPlanMode, and note explicitly anything you could not verify because verifying " +
			"it would have required changing something."
	case permission.ModeBypassPermissions:
		return "\n\n# Permission mode: bypass permissions\n" +
			"Permission checks and the host gate are both off for this session; the user has " +
			"taken responsibility for that. Nothing will stop a destructive command, so the " +
			"judgement that would have been the gate's is now yours."
	default:
		return ""
	}
}

// systemFor returns the system blocks for a turn under mode.
//
// The mode clause is a separate block appended after the base prompt rather
// than spliced into it, so the long, identical prefix stays byte-for-byte
// stable and keeps whatever prompt caching it has earned. A mode change
// invalidates nothing before the breakpoint.
func systemFor(base string, mode permission.Mode) []anthropic.BetaTextBlockParam {
	var out []anthropic.BetaTextBlockParam
	if base != "" {
		out = append(out, anthropic.BetaTextBlockParam{Text: base})
	}
	if clause := modeClause(mode); clause != "" {
		out = append(out, anthropic.BetaTextBlockParam{Text: clause})
	}
	return out
}

// modeTracker reports mode changes once each.
//
// The zero value has no mode, so the first turn of a Run announces whatever it
// starts in. That is wanted rather than noise: a resumed session can start in
// plan mode, and "nothing was said, so it must be autonomous" is exactly the
// assumption that made this invisible.
type modeTracker struct {
	seen    permission.Mode
	started bool
}

// changed records mode and reports whether it differs from the last one seen.
func (t *modeTracker) changed(mode permission.Mode) bool {
	if t.started && t.seen == mode {
		return false
	}
	prev, first := t.seen, !t.started
	t.seen, t.started = mode, true
	return first || prev != mode
}

// announceMode emits a permission_mode event when the mode has changed since
// the last turn.
func announceMode(mode permission.Mode, emit Emitter, t *modeTracker) {
	if !t.changed(mode) || emit == nil {
		return
	}
	emit(Event{Type: "permission_mode", Content: string(mode)})
}
