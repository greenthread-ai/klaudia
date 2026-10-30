package agent

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/permission"
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
// So the mode is told to the model, and a change is announced as an event. The
// loop is the only place that sees the live mode function, which means every
// route that changes it — the picker, /mode, /plan, approving a plan, a
// stream-json control request — is covered without any of them knowing about
// this.
//
// It is told in the conversation, not the system prompt. It used to be a
// system block rebuilt per turn, and on models with preserved thinking a
// changed system prompt invalidates every thinking block before the change:
// an account created on or after 2026-08-31 was refused outright, and with
// drop_block the model lost its reasoning each time the mode moved. So the
// system prompt stays as the host wrote it, and a mode that differs from the
// last one the conversation was told is added to the next user message the
// loop sends, which is append-only. (PR #63; Claude Code delivers plan mode the same way.)

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
			"tools not marked read-only are refused, and so is any command that would write — including builds and " +
			"test runs, which can leave output behind. Commands that only inspect are " +
			"available and you are expected to use them: `git log`, `git diff`, `git status`, " +
			"`ls`, `sed -n`, `go doc`, `go list`. A command nothing can classify will ask the user " +
			"rather than be refused. Research the request properly, then present the plan with " +
			"ExitPlanMode — passing its steps, which become the checklist the user watches once " +
			"it is approved — and note explicitly anything you could not verify because " +
			"verifying it would have required changing something."
	case permission.ModeBypassPermissions:
		return "\n\n# Permission mode: bypass permissions\n" +
			"Permission checks and the host gate are both off for this session; the user has " +
			"taken responsibility for that. Nothing will stop a destructive command, so the " +
			"judgement that would have been the gate's is now yours."
	default:
		return ""
	}
}

// autonomousClause is said only on a change back to autonomous: the earlier
// clause is still in the conversation, and silence would leave it standing.
const autonomousClause = "# Permission mode: autonomous\n" +
	"The permission mode is now autonomous. You may edit files and run commands; the host " +
	"gate still asks before a change to this machine."

// modeHeaders maps each clause's header line back to its mode, so the
// conversation itself records which mode it was last told.
var modeHeaders = map[string]permission.Mode{
	"plan":               permission.ModePlan,
	"bypass permissions": permission.ModeBypassPermissions,
	"autonomous":         permission.ModeAutonomous,
}

// lastToldMode is the mode the conversation last announced, or autonomous
// when it has announced none (the base prompt's assumption).
func lastToldMode(messages []anthropic.BetaMessageParam) permission.Mode {
	const header = "# Permission mode: "
	told := permission.ModeAutonomous
	for _, m := range messages {
		if m.Role != anthropic.BetaMessageParamRoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.OfText == nil {
				continue
			}
			text := b.OfText.Text
			for {
				i := strings.Index(text, header)
				if i < 0 {
					break
				}
				text = text[i+len(header):]
				line, _, _ := strings.Cut(text, "\n")
				if mode, ok := modeHeaders[strings.TrimSpace(line)]; ok {
					told = mode
				}
			}
		}
	}
	return told
}

// tellMode adds the mode clause to msg, a user message about to be sent and
// recorded, when the mode differs from what the conversation was last told.
// It goes in the message itself rather than a message of its own, so the
// transcript holds what was sent and nothing reads it as a prompt the user
// typed.
func tellMode(history []anthropic.BetaMessageParam, msg *anthropic.BetaMessageParam, mode permission.Mode) {
	if lastToldMode(append(history[:len(history):len(history)], *msg)) == mode {
		return
	}
	text := strings.TrimSpace(modeClause(mode))
	if mode == permission.ModeAutonomous {
		text = autonomousClause
	}
	if text != "" {
		msg.Content = append(msg.Content, anthropic.NewBetaTextBlock(text))
	}
}

// systemFor returns the system blocks: the base prompt, unchanged for the
// whole conversation (see the top of this file for why the mode is not here).
// An empty base prompt yields no block, which the API would reject.
func systemFor(base string) []anthropic.BetaTextBlockParam {
	if base == "" {
		return nil
	}
	return []anthropic.BetaTextBlockParam{{Text: base}}
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
	emit(Event{Type: EventPermissionMode, Content: string(mode)})
}
