package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/pkg/permission"
)

// §19: resume the work, not merely the chat.
//
// Reloading a transcript restores what was *said*. It restores none of what was
// *true*: which files are half-changed, what was running, what the user had
// agreed Klaudia could do to their machine. Someone who resumes and types
// "carry on" is relying on all three, and the gap between the conversation and
// the world is where a resumed session goes wrong.
//
// So the banner reconciles rather than reports. Everything in it is observed at
// the moment of resume, not remembered — a stored claim that a server is
// running would be false the instant the process exited, with no later moment
// at which it could correct itself. And the things that did *not* survive are
// named at least as plainly as the things that did, because the conversation
// the user is about to continue implies the server is still up.

// resumeState is the operational picture at resume.
type resumeState struct {
	Goal        string
	Branch      string
	KlaudiaWork []string // still-dirty files Klaudia changed
	UserWork    []string // still-dirty files it did not
	DeadJobs    []string // jobs the transcript started, none of which survived
	// HasGuardrail is false only when the session was built without a gate at
	// all. There is no posture to report beyond that: the gate always
	// classifies, and skipping it is a property of the mode.
	HasGuardrail bool
	Mode         permission.Mode
	Messages     int
}

// buildResumeState assembles the picture from the world and the transcript.
func (m *Model) buildResumeState() resumeState {
	st := resumeState{Messages: len(m.history), Mode: m.currentMode()}
	if m.sess != nil {
		st.Goal = m.sess.Goal
		st.Branch = m.sess.GitBranch
		st.HasGuardrail = m.sess.Trust != nil
	}

	// Ownership first: without re-seeding, every file Klaudia wrote yesterday
	// looks like the user's today and /commit would refuse to stage its own
	// work.
	m.seedOwnershipFromHistory(historyEditedPaths(m.history))

	if m.sess != nil && m.sess.CWD != "" {
		if status, err := gitOutput(m.sess.CWD, "status", "--porcelain"); err == nil {
			for _, f := range m.classify(status) {
				if f.Owner == ownerUser {
					st.UserWork = append(st.UserWork, f.Path)
					continue
				}
				st.KlaudiaWork = append(st.KlaudiaWork, f.Path)
			}
		}
	}

	// Jobs never survive: they were children of a process that has exited.
	st.DeadJobs = historyJobNames(m.history)
	return st
}

// render is the banner.
func (st resumeState) render() string {
	var b strings.Builder
	// One line always, because the conversation is back even when nothing
	// else is: a resumed session with a clean tree used to show no sign of
	// it, while the model carried on from history the user could not see.
	// Approvals are named here because they are the one thing that does not
	// come back — session-scoped by design, so a permission granted yesterday
	// never silently applies today.
	fmt.Fprintf(&b, "↻ continuing the last session here · %s · approvals start fresh · /clear to start over",
		countOf(st.Messages, "message"))

	// The rest only when there is something to say, one line each. The card
	// was ten lines on every start, most of them restating the mode.
	if st.Goal != "" {
		b.WriteString("\n  goal: " + oneline(st.Goal, 100))
	}
	if len(st.KlaudiaWork) > 0 || len(st.UserWork) > 0 {
		b.WriteString("\n  uncommitted")
		if st.Branch != "" {
			b.WriteString(" on " + st.Branch)
		}
		b.WriteString(":")
		var parts []string
		if n := len(st.KlaudiaWork); n > 0 {
			parts = append(parts, fmt.Sprintf("%s by Klaudia (%s)", countOf(n, "file"), shortList(st.KlaudiaWork, 3)))
		}
		if n := len(st.UserWork); n > 0 {
			parts = append(parts, countOf(n, "file")+" of yours")
		}
		b.WriteString(" " + strings.Join(parts, " · ") + " — /changes")
	}
	if len(st.DeadJobs) > 0 {
		// Named as stopped, never running: they were children of a process
		// that exited, and the conversation implies they are still up.
		b.WriteString("\n  stopped when the previous session ended: " + strings.Join(st.DeadJobs, ", "))
	}
	switch {
	case !st.HasGuardrail:
		b.WriteString("\n  ⚠ no host guardrail is configured")
	case st.Mode == permission.ModeBypassPermissions:
		b.WriteString("\n  ⚠ bypassPermissions: the host guardrail is off")
	}
	return b.String()
}

// shortList joins up to n names, then "+N more".
func shortList(names []string, n int) string {
	if len(names) <= n {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:n], ", ") + fmt.Sprintf(", +%d more", len(names)-n)
}

// seedOwnershipFromHistory re-attributes files Klaudia wrote in a past session.
//
// Deliberately does NOT stamp them. Klaudia has no idea what happened to a file
// between sessions, so it cannot claim the file is unchanged since it wrote it.
// An unstamped path that is also in the baseline reads as shared — and shared
// is the honest answer, because it stops undo touching something that may have
// been edited by hand in between.
func (m *Model) seedOwnershipFromHistory(paths []string) {
	if len(paths) == 0 {
		return
	}
	if m.touched == nil {
		m.touched = map[string]bool{}
	}
	for _, p := range paths {
		if rel := m.relToRepo(p); rel != "" && rel != "." {
			m.touched[rel] = true
		}
	}
}

// historyEditedPaths finds the files past turns wrote, from their tool_use
// blocks.
func historyEditedPaths(history []anthropic.BetaMessageParam) []string {
	seen := map[string]bool{}
	var out []string
	forEachToolUse(history, func(name string, input json.RawMessage) {
		switch name {
		case "Write", "Edit", "NotebookEdit":
		default:
			return
		}
		var in struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return
		}
		for _, p := range []string{in.FilePath, in.NotebookPath} {
			if p != "" && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	})
	return out
}

// historyJobNames finds jobs the previous session started, from the tool
// results it recorded.
//
// Read out of the transcript rather than stored anywhere. A session file that
// recorded "dev is running" would be wrong the moment the process exited, and
// nothing would ever correct it.
func historyJobNames(history []anthropic.BetaMessageParam) []string {
	var texts []string
	for _, msg := range history {
		for _, blk := range msg.Content {
			if blk.OfToolResult == nil {
				continue
			}
			for _, c := range blk.OfToolResult.Content {
				if c.OfText != nil {
					texts = append(texts, c.OfText.Text)
				}
			}
		}
	}
	return jobNamesFromResults(texts)
}

// jobNamesFromResults pulls names out of the Bash tool's start/restart lines.
func jobNamesFromResults(texts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range texts {
		for _, marker := range []string{"Started job ", "Restarted job "} {
			rest := t
			for {
				i := strings.Index(rest, marker)
				if i < 0 {
					break
				}
				rest = rest[i+len(marker):]
				name := strings.TrimSpace(rest)
				if cut := strings.IndexAny(name, " (.\n"); cut >= 0 {
					name = name[:cut]
				}
				if name == "" || seen[name] {
					continue
				}
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// forEachToolUse walks the tool_use blocks in a history.
func forEachToolUse(history []anthropic.BetaMessageParam, fn func(name string, input json.RawMessage)) {
	for _, msg := range history {
		for _, blk := range msg.Content {
			if blk.OfToolUse == nil {
				continue
			}
			raw, err := json.Marshal(blk.OfToolUse.Input)
			if err != nil {
				continue
			}
			fn(blk.OfToolUse.Name, raw)
		}
	}
}
