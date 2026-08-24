package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/greenthread-ai/klaudia/internal/agent"
	"github.com/greenthread-ai/klaudia/internal/permission"
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
	TrustPolicy agent.HostPolicy
	Mode        permission.Mode
	Messages    int
}

// hasContent reports whether there is anything worth showing. A fresh session
// with an empty tree gets the ordinary banner instead.
func (st resumeState) hasContent() bool {
	return st.Goal != "" || len(st.KlaudiaWork) > 0 || len(st.UserWork) > 0 || len(st.DeadJobs) > 0
}

// buildResumeState assembles the picture from the world and the transcript.
func (m *Model) buildResumeState() resumeState {
	st := resumeState{Messages: len(m.history), Mode: m.currentMode()}
	if m.sess != nil {
		st.Goal = m.sess.Goal
		st.Branch = m.sess.GitBranch
		if m.sess.Trust != nil {
			st.TrustPolicy = m.sess.Trust.Policy()
		}
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
	b.WriteString("Resuming\n")

	if st.Goal != "" {
		b.WriteString("\nGoal\n  " + st.Goal + "\n")
	}

	if len(st.KlaudiaWork) > 0 || len(st.UserWork) > 0 {
		b.WriteString("\nWorking tree\n")
		if n := len(st.KlaudiaWork); n > 0 {
			fmt.Fprintf(&b, "  %d Klaudia change(s)\n", n)
			for _, f := range st.KlaudiaWork {
				b.WriteString("    " + f + "\n")
			}
		}
		if n := len(st.UserWork); n > 0 {
			fmt.Fprintf(&b, "  %d of your change(s) — /changes to see the split\n", n)
		}
		if st.Branch != "" {
			b.WriteString("  on " + st.Branch + "\n")
		}
	}

	if len(st.DeadJobs) > 0 {
		b.WriteString("\nJobs\n")
		for _, j := range st.DeadJobs {
			b.WriteString("  " + j + "  stopped when the previous session ended\n")
		}
	}

	b.WriteString("\nTrust\n")
	switch st.TrustPolicy {
	case agent.HostEnforce:
		b.WriteString("  host changes need your agreement\n")
	case agent.HostObserve:
		b.WriteString("  observing only — nothing is stopped\n")
	case agent.HostOff:
		b.WriteString("  guardrail off\n")
	default:
		b.WriteString("  not configured\n")
	}
	fmt.Fprintf(&b, "  %s mode\n", st.Mode)
	// The one thing that must NOT come back. Approvals are session-scoped by
	// design; resurrecting them would mean a permission granted yesterday
	// silently applying today — exactly the "flaky remembered command
	// permissions" the trust model replaced.
	b.WriteString("  previous approvals were not restored — Klaudia will ask again\n")

	return strings.TrimRight(b.String(), "\n")
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
