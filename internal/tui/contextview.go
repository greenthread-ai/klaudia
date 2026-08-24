package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// §18: show useful context, not merely token counts.
//
// "You are at 62% of the context window" tells the user a number they cannot
// act on. What they can act on is *what is in it*: which files Klaudia has
// actually read, which it is working in, and which it has not looked at
// despite them mattering. Token accounting stays available in /stats, where
// someone who wants it knows to look.
//
// Pinning is the one lever that changes behaviour rather than just reporting
// it: a pinned file is re-stated to the model every turn and survives
// compaction, which is what makes "the architecture doc" stay relevant after
// forty turns instead of quietly falling out of the window.

// pin adds a path to the pinned set.
func (m *Model) pin(path string) (string, bool) {
	rel := m.relToRepo(strings.TrimSpace(path))
	if rel == "" || rel == "." {
		return "", false
	}
	for _, p := range m.pinned {
		if p == rel {
			return rel, false // already pinned
		}
	}
	m.pinned = append(m.pinned, rel)
	sort.Strings(m.pinned)
	return rel, true
}

// unpin removes a path.
func (m *Model) unpin(path string) (string, bool) {
	rel := m.relToRepo(strings.TrimSpace(path))
	for i, p := range m.pinned {
		if p == rel {
			m.pinned = append(m.pinned[:i], m.pinned[i+1:]...)
			return rel, true
		}
	}
	return rel, false
}

// pinnedBlock renders the pinned files for the prompt.
//
// Re-stated every turn rather than injected once: a file mentioned forty turns
// ago is, for practical purposes, not in context at all, and compaction is
// allowed to elide it. This is the mechanism behind §18's "critical
// instructions survive compaction".
func (m *Model) pinnedBlock() string {
	if len(m.pinned) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Files the user has pinned as important for this task. " +
		"Read them if you have not already, and keep them in mind:\n")
	for _, p := range m.pinned {
		full := p
		if m.sess != nil && m.sess.CWD != "" {
			full = filepath.Join(m.sess.CWD, p)
		}
		if _, err := os.Stat(full); err != nil {
			fmt.Fprintf(&b, "  %s (no longer present)\n", p)
			continue
		}
		fmt.Fprintf(&b, "  %s\n", p)
	}
	return strings.TrimRight(b.String(), "\n")
}

// activeAreas summarises where the work has been, as directory globs rather
// than a list of forty files.
//
// A user scanning /context wants "src/auth/*" — one line that says where
// Klaudia has been living. Twelve individual paths under the same directory
// convey the same thing and take twelve times the room.
func activeAreas(paths []string) []string {
	byDir := map[string]int{}
	for _, p := range paths {
		dir := filepath.Dir(p)
		if dir == "." {
			dir = ""
		}
		byDir[dir]++
	}
	var out []string
	for dir, n := range byDir {
		switch {
		case dir == "" && n == 1:
			continue // a single top-level file is not an "area"
		case dir == "":
			out = append(out, fmt.Sprintf("*  (%d files)", n))
		case n == 1:
			out = append(out, dir+"/")
		default:
			out = append(out, fmt.Sprintf("%s/*  (%d files)", dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// renderContext is /context.
func (m *Model) renderContext() string {
	var b strings.Builder
	b.WriteString("Context\n")

	// Where, first. §13 asks that the working directory always be
	// understandable, and this is the place someone looks for it.
	cwd, branch, sid := "(unknown)", "(none)", "(unknown)"
	if m.sess != nil {
		if m.sess.CWD != "" {
			cwd = m.sess.CWD
		}
		if m.sess.GitBranch != "" {
			branch = m.sess.GitBranch
		}
		if m.sess.SessionID != "" {
			sid = m.sess.SessionID
		}
	}
	fmt.Fprintf(&b, "  cwd=%s\n  git-branch=%s\n  session-id=%s\n  messages=%d\n",
		cwd, branch, sid, len(m.history))
	for _, d := range m.sess.ExtraDirs {
		b.WriteString("  also=" + d + "\n")
	}

	if len(m.pinned) > 0 {
		b.WriteString("\nPinned\n")
		for _, p := range m.pinned {
			note := ""
			if m.sess != nil && m.sess.CWD != "" {
				if _, err := os.Stat(filepath.Join(m.sess.CWD, p)); err != nil {
					note = "  (no longer present)"
				}
			}
			b.WriteString("  " + p + note + "\n")
		}
	}

	if changed := sortedKeys(m.touched); len(changed) > 0 {
		b.WriteString("\nChanged this session\n")
		for _, p := range changed {
			b.WriteString("  " + p + "\n")
		}
	}

	if areas := activeAreas(m.recentPaths); len(areas) > 0 {
		b.WriteString("\nActive\n")
		for _, a := range areas {
			b.WriteString("  " + a + "\n")
		}
	}

	if seen := m.recentlyInspected(); len(seen) > 0 {
		b.WriteString("\nRecently inspected\n")
		for _, p := range seen {
			b.WriteString("  " + p + "\n")
		}
	}

	// The honest footer. Everything above is what Klaudia has looked at; this
	// says what that does not amount to.
	b.WriteString("\n" + m.contextCaveat())
	return strings.TrimRight(b.String(), "\n")
}

// recentlyInspected lists files Klaudia read but did not change, newest first
// and bounded — this is orientation, not an audit log.
func (m *Model) recentlyInspected() []string {
	var out []string
	for _, p := range m.recentPaths {
		if m.touched[p] {
			continue
		}
		out = append(out, p)
		if len(out) == 8 {
			break
		}
	}
	return out
}

// contextCaveat states what the listing does not prove.
//
// §18's last checkbox is "Klaudia admits when it hasn't inspected something
// important". It cannot know what is important, but it can say plainly that
// this list is what it looked at rather than what the task needed — which is
// the claim a user would otherwise infer.
func (m *Model) contextCaveat() string {
	n := len(m.recentPaths)
	switch {
	case n == 0:
		return "Klaudia has not read any files yet — anything it says about this code is a guess."
	default:
		return fmt.Sprintf(
			"This is what Klaudia has read (%d files), not what the task needs. "+
				"If something matters and is not here, /pin it.", n)
	}
}

// forget drops a path from the recorded context.
//
// It cannot remove what the model has already seen — the message history is the
// message history — so it says so rather than implying otherwise. What it does
// do is stop the file being re-stated, ranked, or listed.
func (m *Model) forget(path string) (string, bool) {
	rel := m.relToRepo(strings.TrimSpace(path))
	found := false
	for i, p := range m.recentPaths {
		if p == rel {
			m.recentPaths = append(m.recentPaths[:i], m.recentPaths[i+1:]...)
			found = true
			break
		}
	}
	if _, ok := m.unpin(rel); ok {
		found = true
	}
	return rel, found
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// pinCommand handles /pin and /unpin.
func (m *Model) pinCommand(args []string, add bool) {
	if len(args) == 0 {
		if len(m.pinned) == 0 {
			m.appendLine(bannerStyle.Render("Nothing pinned. /pin <path> keeps a file in context every turn."))
			return
		}
		m.appendLine(bannerStyle.Render("Pinned:\n  " + strings.Join(m.pinned, "\n  ")))
		return
	}
	path := strings.Join(args, " ")
	if add {
		rel, ok := m.pin(path)
		if !ok {
			m.appendLine(bannerStyle.Render(rel + " is already pinned."))
			return
		}
		note := ""
		if m.sess != nil && m.sess.CWD != "" {
			if _, err := os.Stat(filepath.Join(m.sess.CWD, rel)); err != nil {
				note = " (not found — pinned anyway, in case you are about to create it)"
			}
		}
		m.appendLine(bannerStyle.Render("Pinned " + rel + note +
			". It will be re-stated every turn and will survive compaction."))
		return
	}
	rel, ok := m.unpin(path)
	if !ok {
		m.appendLine(errStyle.Render(rel + " was not pinned."))
		return
	}
	m.appendLine(bannerStyle.Render("Unpinned " + rel + "."))
}

// forgetCommand handles /forget.
func (m *Model) forgetCommand(args []string) {
	if len(args) == 0 {
		m.appendLine(errStyle.Render("usage: /forget <path>"))
		return
	}
	rel, ok := m.forget(strings.Join(args, " "))
	if !ok {
		m.appendLine(bannerStyle.Render(rel + " is not in the tracked context."))
		return
	}
	m.appendLine(bannerStyle.Render("Dropped " + rel + " from the tracked context. " +
		"Anything Klaudia has already read stays in the conversation — /compact to shrink that."))
}
