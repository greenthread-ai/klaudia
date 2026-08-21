package tui

import (
	"path/filepath"
	"sort"
	"strings"
)

// /commit used to run `git add -A`, which sweeps up everything in the working
// tree — the half-finished change the user left open in another editor, the
// scratch file they meant to delete, the unrelated fix they were saving for a
// separate commit. All of it, into a commit whose message describes something
// else entirely.
//
// The fix is to commit what Klaudia actually changed. The TUI already sees every
// Edit/Write/NotebookEdit go past, so the set of files this session touched is
// known; anything else in the tree belongs to the user and is listed, not
// staged. When the user has staged something themselves, that wins outright:
// an explicit `git add` is a statement about what belongs in the commit, and
// second-guessing it would be the same mistake in the other direction.

// noteTouched records a path Klaudia modified this session.
func (m *Model) noteTouched(path string) {
	if path == "" {
		return
	}
	if m.touched == nil {
		m.touched = map[string]bool{}
	}
	m.touched[m.relToRepo(path)] = true
}

// relToRepo normalises a path to the form `git status --short` reports, so the
// two can be compared.
func (m *Model) relToRepo(path string) string {
	if m.sess == nil || m.sess.CWD == "" {
		return filepath.Clean(path)
	}
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(m.sess.CWD, abs)
	}
	rel, err := filepath.Rel(m.sess.CWD, abs)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.ToSlash(rel)
}

// commitPlan is what /commit intends to do.
type commitPlan struct {
	// Staged is true when the user staged changes themselves; Klaudia commits
	// exactly those and stages nothing.
	Staged bool
	// Add lists the paths to stage — files Klaudia changed that are dirty.
	Add []string
	// Skipped lists dirty paths Klaudia did not change, which stay out of the
	// commit. Shown so the user can see what is being left behind rather than
	// discovering it later.
	Skipped []string
}

// planCommit decides what goes into the commit from `git status --porcelain`
// output and the set of files this session touched.
func planCommit(status string, touched map[string]bool) commitPlan {
	var plan commitPlan
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 3 {
			continue
		}
		x, y := line[0], line[1]
		path := unquoteStatusPath(strings.TrimSpace(line[2:]))
		// A rename reads "R  old -> new"; the new name is the one to stage.
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		// Anything already in the index is the user's decision, and it stands.
		if x != ' ' && x != '?' {
			plan.Staged = true
			continue
		}
		if touched[path] {
			plan.Add = append(plan.Add, path)
			continue
		}
		// An untracked file Klaudia did not create is not ours to add. Neither
		// is a modification it did not make.
		_ = y
		plan.Skipped = append(plan.Skipped, path)
	}
	// An index the user built themselves is the whole answer. Anything Klaudia
	// changed but left unstaged is being left out of this commit, so it moves to
	// the reported-not-staged list rather than being quietly added on top.
	if plan.Staged {
		plan.Skipped = append(plan.Skipped, plan.Add...)
		plan.Add = nil
	}
	sort.Strings(plan.Add)
	sort.Strings(plan.Skipped)
	return plan
}

// unquoteStatusPath undoes git's C-style quoting of paths with unusual bytes.
// Without this, a touched file with a space or a non-ASCII name would never
// match and would silently be left out of the commit.
func unquoteStatusPath(p string) string {
	if len(p) < 2 || p[0] != '"' || p[len(p)-1] != '"' {
		return p
	}
	inner := p[1 : len(p)-1]
	var b strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' || i+1 >= len(inner) {
			b.WriteByte(inner[i])
			continue
		}
		i++
		switch inner[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '"', '\\':
			b.WriteByte(inner[i])
		default:
			// Octal escape for a raw byte (\303 …). Keep the byte.
			if i+2 < len(inner) && isOctal(inner[i]) && isOctal(inner[i+1]) && isOctal(inner[i+2]) {
				v := (int(inner[i]-'0') << 6) | (int(inner[i+1]-'0') << 3) | int(inner[i+2]-'0')
				b.WriteByte(byte(v))
				i += 2
				continue
			}
			b.WriteByte(inner[i])
		}
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// describe renders the plan for the confirmation prompt.
func (p commitPlan) describe() string {
	var b strings.Builder
	switch {
	case p.Staged:
		b.WriteString("Commit what you have staged?")
	case len(p.Add) > 0:
		b.WriteString("Commit the files Klaudia changed?\n")
		for _, f := range p.Add {
			b.WriteString("  + " + f + "\n")
		}
	default:
		b.WriteString("Nothing to commit that Klaudia changed.")
	}
	if len(p.Skipped) > 0 {
		b.WriteString("\nNot included:\n")
		for _, f := range p.Skipped {
			b.WriteString("  · " + f + "\n")
		}
		b.WriteString("  (stage them yourself first if you want them in)")
	}
	return strings.TrimRight(b.String(), "\n")
}

// empty reports whether there is nothing this command can commit.
func (p commitPlan) empty() bool { return !p.Staged && len(p.Add) == 0 }
