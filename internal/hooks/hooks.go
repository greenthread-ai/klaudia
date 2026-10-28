// Package hooks runs user-configured shell commands at fixed points in a turn.
//
// A hook is the escape hatch for everything Klaudia should not have an opinion
// about: run the project's formatter after a write, refuse edits to generated
// files, paste the current ticket into every prompt, print a desktop
// notification when a long run finishes. None of that belongs in the agent, and
// all of it is cheap to express as a command.
//
// # What a hook is not
//
// It is not a security boundary, and it must not be mistaken for one. The host
// gate (internal/agent/hostgate.go) decides whether a tool call may change this
// machine, and internal/sandbox is the only kernel-enforced answer. A PreToolUse
// hook sits *beside* the gate rather than inside it: it runs after the gate and
// the permission check have both allowed the call, so a hook can narrow what
// Klaudia will do but can never widen it. That ordering is the whole reason
// hooks are allowed to block at all — a config file that could grant permission
// would be a way to turn the gate off by writing a file.
//
// # The four events
//
// Deliberately few. Claude Code has grown past thirty lifecycle events, and the
// long tail of them exists to serve one workflow each; every one is a promise
// about when the loop will call out, which is a promise about the loop's shape.
// These four are the ones that cannot be expressed any other way:
//
//   - SessionStart    — before the first request. Adds context.
//   - UserPromptSubmit — a prompt is about to be sent. Adds context, or blocks.
//   - PreToolUse      — a tool call is about to run. Blocks.
//   - PostToolUse     — a tool call finished. Adds feedback to the result.
//
// Stop/SubagentStop — "the model thinks it is done, make it keep going" — are
// absent on purpose: a hook that can deny completion can hang a session, and
// nothing in a shell command knows better than the loop whether the work is
// finished.
package hooks

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/greenthread-ai/klaudia/internal/config"
)

// Event is a lifecycle point a hook can be attached to.
type Event string

const (
	SessionStart     Event = "SessionStart"
	UserPromptSubmit Event = "UserPromptSubmit"
	PreToolUse       Event = "PreToolUse"
	PostToolUse      Event = "PostToolUse"
	// Notification fires when Klaudia needs the user — a permission prompt,
	// a question, a plan to approve, a long turn finished — and the terminal
	// is not focused. It cannot block: it is for telling someone elsewhere
	// (a phone, a chat), and the payload carries the reason and a message.
	Notification Event = "Notification"
)

// Events lists every event, in lifecycle order. Used for validation messages
// and by /doctor.
var Events = []Event{SessionStart, UserPromptSubmit, PreToolUse, PostToolUse, Notification}

// canBlock reports whether a refusal from this event's hooks means anything.
//
// SessionStart cannot block because there is nothing to block: the session is
// already starting, the user asked for it, and the only honest response to
// "your SessionStart hook said no" is to tell them and carry on. Treating its
// exit 2 as a veto would make a typo in a config file look like a broken
// install.
func (e Event) canBlock() bool { return e != SessionStart && e != Notification }

// matchesTools reports whether this event's hooks are selected by tool name.
func (e Event) matchesTools() bool { return e == PreToolUse || e == PostToolUse }

// defaultTimeout bounds a hook that does not set its own.
//
// Thirty seconds, not the minute Claude Code allows. A hook is in the critical
// path of a turn — PreToolUse runs before every single tool call — so the cost
// of one that hangs is a session that looks frozen, and the formatter and
// linter cases that motivate the feature finish in under a second. A hook that
// genuinely needs longer can say so; the default protects the person who did
// not think about it.
const defaultTimeout = 30 * time.Second

// Hook is one compiled, runnable hook.
type Hook struct {
	Event   Event
	Command string
	Timeout time.Duration
	// Source names the file this came from, for error messages and for the
	// approval prompt. Hooks are the one config entry where a wrong value gets
	// *executed*, so every diagnostic says which file to go and fix.
	Source string
	// Project marks a hook that came from the repository rather than from the
	// user's own config. See trust.go.
	Project bool

	// matcher is nil when the hook matches every tool.
	matcher *regexp.Regexp
	// raw is the matcher as written, kept for the fingerprint and for display.
	raw string
}

// Matcher returns the hook's tool-name pattern as configured ("" for all).
func (h Hook) Matcher() string { return h.raw }

// matches reports whether the hook applies to a call of the named tool.
func (h Hook) matches(tool string) bool {
	if !h.Event.matchesTools() || h.matcher == nil {
		return true
	}
	return h.matcher.MatchString(tool)
}

// String renders the hook the way the approval prompt and /doctor show it.
func (h Hook) String() string {
	if h.raw != "" {
		return fmt.Sprintf("%s(%s): %s", h.Event, h.raw, h.Command)
	}
	return fmt.Sprintf("%s: %s", h.Event, h.Command)
}

// compile turns on-disk entries into runnable hooks, reporting every problem it
// finds rather than the first.
//
// A bad entry is dropped, not fatal, and the reason is surfaced to the user.
// The alternative — refusing to start, or silently ignoring it — both fail the
// same way: a mistyped event name is the likeliest error by a distance, and
// "SessionStarted" quietly never firing is the worst outcome available.
func compile(entries []config.Hook, source string, project bool) ([]Hook, []error) {
	var out []Hook
	var errs []error
	for i, e := range entries {
		where := fmt.Sprintf("%s: hooks[%d]", source, i)
		ev := Event(strings.TrimSpace(e.Event))
		if !valid(ev) {
			errs = append(errs, fmt.Errorf("%s: unknown event %q (want one of %s)", where, e.Event, eventList()))
			continue
		}
		cmd := strings.TrimSpace(e.Command)
		if cmd == "" {
			errs = append(errs, fmt.Errorf("%s: %s hook has no command", where, ev))
			continue
		}
		h := Hook{Event: ev, Command: cmd, Timeout: defaultTimeout, Source: source, Project: project}
		if m := strings.TrimSpace(e.Matcher); m != "" {
			if !ev.matchesTools() {
				// Not an error worth dropping the hook for, but worth saying:
				// a matcher on SessionStart looks like it works and does
				// nothing, which is how a hook comes to fire on every prompt
				// when its author believed it was scoped to one tool.
				errs = append(errs, fmt.Errorf("%s: %s hooks have no tool to match, so matcher %q is ignored", where, ev, m))
			} else {
				re, err := regexp.Compile(m)
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: bad matcher %q: %w", where, m, err))
					continue
				}
				h.matcher = re
				h.raw = m
			}
		}
		if t := strings.TrimSpace(e.Timeout); t != "" {
			d, err := time.ParseDuration(t)
			switch {
			case err != nil:
				errs = append(errs, fmt.Errorf("%s: bad timeout %q: %w", where, t, err))
				continue
			case d <= 0:
				errs = append(errs, fmt.Errorf("%s: timeout %q is not positive", where, t))
				continue
			}
			h.Timeout = d
		}
		out = append(out, h)
	}
	return out, errs
}

func valid(e Event) bool {
	for _, k := range Events {
		if k == e {
			return true
		}
	}
	return false
}

func eventList() string {
	names := make([]string, len(Events))
	for i, e := range Events {
		names[i] = string(e)
	}
	return strings.Join(names, ", ")
}
