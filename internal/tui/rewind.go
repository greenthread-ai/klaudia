package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// /rewind goes back to one of this session's earlier prompts: the files
// Klaudia changed from that turn on are put back as they were before it, the
// conversation is cut back to before the prompt, and the prompt is returned to
// the input box to edit or send again. "Go back to before I asked for that" is
// one action, where /undo was one turn at a time and left the conversation
// believing the work was still there.
//
// The rules are /undo's. A file the user also changed is left alone and said
// so; the plan is shown before anything is written; the cut is recorded in the
// transcript, so a resumed session stays rewound. Only prompts from this
// process are listed: the checkpoints that make the file half possible live in
// memory, and offering an earlier point whose files could not be restored
// would rewind the conversation and not the work.

// rewindPoint is one prompt the user sent this session.
type rewindPoint struct {
	prompt string    // as typed
	hist   int       // len(history) when it was sent: where the cut goes
	turn   int       // turnSeq of that turn: checkpoints from here on are undone
	at     time.Time // when it was sent
}

// noteRewindPoint records a prompt the user sent. Turns Klaudia starts itself
// (a goal loop's iterations, a background result, a job report) are not
// points anyone would go back to, and are left out.
func (m *Model) noteRewindPoint(prompt string) {
	if m.loopRemaining > 0 || m.loopWrapUp || strings.HasPrefix(strings.TrimSpace(prompt), "<") {
		return
	}
	m.rewindPoints = append(m.rewindPoints, rewindPoint{
		prompt: prompt, hist: len(m.history), turn: m.turnSeq, at: time.Now(),
	})
}

// rewindCommand handles /rewind: a picker of earlier prompts, newest first.
func (m *Model) rewindCommand() {
	if m.busyGuard("/rewind") {
		return
	}
	if len(m.rewindPoints) == 0 {
		m.appendLine(bannerStyle.Render("Nothing to rewind to yet — /rewind lists the prompts you have sent this session."))
		return
	}
	items := make([]choiceItem, 0, len(m.rewindPoints))
	for i := len(m.rewindPoints) - 1; i >= 0; i-- {
		p := m.rewindPoints[i]
		label := fmt.Sprintf("%s · %s ago", oneline(p.prompt, 60), elapsed(time.Since(p.at).Truncate(time.Second)))
		items = append(items, choiceItem{label: label, apply: func() string {
			m.confirmRewind(p)
			return ""
		}})
	}
	m.startChoice("Rewind to before which prompt? (Esc to leave as-is)", items)
}

// confirmRewind shows what rewinding to p would do and asks.
func (m *Model) confirmRewind(p rewindPoint) {
	plan := m.planRewind(p.turn)
	var b strings.Builder
	fmt.Fprintf(&b, "Rewind to before %q\n", oneline(p.prompt, 60))
	fmt.Fprintf(&b, "  conversation  back to %s (the prompt returns to the input box)\n", countOf(p.hist, "message"))
	if plan.Empty() && len(plan.Skipped) == 0 {
		// Checkpoints are git blobs. Outside a repository there are none, and
		// saying nothing would read as "no files changed".
		if _, err := gitOutput(m.sess.CWD, "rev-parse", "--git-dir"); err != nil {
			b.WriteString("  files         not restored — this is not a git repository, so Klaudia keeps no checkpoints\n")
		}
	}
	for _, f := range plan.Restore {
		fmt.Fprintf(&b, "  restore  %s\n", f.Path)
	}
	for _, f := range plan.Delete {
		fmt.Fprintf(&b, "  delete   %s  (Klaudia created it)\n", f)
	}
	if len(plan.Skipped) > 0 {
		b.WriteString("\nLeaving alone — you changed these too:\n")
		for _, f := range plan.Skipped {
			b.WriteString("  · " + f + "\n")
		}
	}
	m.appendLine(askStyle.Render(strings.TrimRight(b.String(), "\n")))
	m.confirmAction = func() string { return m.applyRewind(p, plan) }
	m.setState(stateAwaitingConfirm)
}

// planRewind merges every checkpoint from turn on into one undo plan: each
// file goes back to its state before the earliest of those turns touched it.
func (m *Model) planRewind(turn int) undoPlan {
	target := map[string]checkpointFile{}
	var order []string
	m.checkpoints.mu.Lock()
	for _, cp := range m.checkpoints.items {
		if cp.Turn < turn {
			continue
		}
		for _, f := range cp.Files {
			if _, seen := target[f.Path]; !seen { // oldest first: the first is the earliest state
				target[f.Path] = f
				order = append(order, f.Path)
			}
		}
	}
	m.checkpoints.mu.Unlock()

	plan := undoPlan{}
	if len(order) == 0 {
		return plan
	}
	status, _ := gitOutput(m.sess.CWD, "status", "--porcelain")
	owner := map[string]fileOwner{}
	for _, f := range m.classify(status) {
		owner[f.Path] = f.Owner
	}
	for _, path := range order {
		f := target[path]
		if o, ok := owner[path]; ok && o != ownerKlaudia {
			plan.Skipped = append(plan.Skipped, path)
			continue
		}
		if f.New {
			if _, err := os.Stat(filepath.Join(m.sess.CWD, path)); err == nil {
				plan.Delete = append(plan.Delete, path)
			}
			continue
		}
		if cur, err := gitOutput(m.sess.CWD, "hash-object", "--", path); err == nil && strings.TrimSpace(cur) == f.Blob {
			continue
		}
		plan.Restore = append(plan.Restore, f)
	}
	return plan
}

// applyRewind restores the files, cuts the conversation, and hands the prompt
// back.
func (m *Model) applyRewind(p rewindPoint, plan undoPlan) string {
	files := ""
	if !plan.Empty() || len(plan.Skipped) > 0 {
		files = " " + m.restorePlan(plan)
	}
	if p.hist <= len(m.history) {
		m.history = m.history[:p.hist]
	}
	if m.sess != nil && m.sess.OnRewind != nil {
		m.sess.OnRewind(len(m.history))
	}
	m.checkpoints.dropFrom(p.turn)
	for i, rp := range m.rewindPoints {
		if rp.turn >= p.turn {
			m.rewindPoints = m.rewindPoints[:i]
			break
		}
	}
	m.residentTokens, m.contextMeasured = 0, false
	m.input.SetValue(p.prompt)
	m.input.CursorEnd()
	return "Rewound to before that prompt; it is back in the input box." + files
}

// dropFrom removes the checkpoints of turn and later, which a rewind has
// either restored or deliberately left alone.
func (s *checkpointStack) dropFrom(turn int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.items[:0]
	for _, cp := range s.items {
		if cp.Turn < turn {
			kept = append(kept, cp)
		}
	}
	s.items = kept
}
