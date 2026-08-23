package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// §16: whose change is this?
//
// A working tree is a shared surface. The user had two files half-edited before
// Klaudia started; Klaudia touched four more; and while it was working the user
// fixed a typo in a third. By the end nobody can tell those apart from `git
// status`, which is how an undo eats someone's afternoon and how a commit
// message ends up describing a third of its own diff.
//
// Three facts make the difference, and Klaudia can know all three cheaply:
// what was already dirty when the session started, what Klaudia wrote, and
// whether anything changed underneath it. The rest is arithmetic.

// fileOwner says who is responsible for a working-tree change.
type fileOwner int

const (
	ownerUser    fileOwner = iota // dirty before Klaudia started, or edited by hand since
	ownerKlaudia                  // Klaudia wrote it this session
	ownerBoth                     // Klaudia wrote it and it changed underneath
)

// ownedFile is one path and who changed it.
type ownedFile struct {
	Path   string
	Owner  fileOwner
	Status string // the two-letter code from git status --porcelain
}

// baseline is the working tree as it was when the session began.
//
// Captured once. Without it, "the user's existing changes" is unknowable: a
// dirty file looks identical whether the user left it that way an hour ago or
// Klaudia wrote it a minute ago.
type baseline struct {
	dirty  map[string]bool      // paths already modified at session start
	stamps map[string]fileStamp // what Klaudia last wrote, to detect edits underneath
}

// fileStamp is enough to notice a file changing without hashing it on every
// check: size plus modification time. A user editing a file in another window
// changes both.
type fileStamp struct {
	size int64
	mod  time.Time
}

func newBaseline() *baseline {
	return &baseline{dirty: map[string]bool{}, stamps: map[string]fileStamp{}}
}

// capture records which files were already dirty. Called once at startup.
func (b *baseline) capture(status string) {
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 3 {
			continue
		}
		path := unquoteStatusPath(strings.TrimSpace(line[2:]))
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		b.dirty[path] = true
	}
}

// stamp records the state of a file Klaudia just wrote.
func (b *baseline) stamp(dir, rel string) {
	info, err := os.Stat(filepath.Join(dir, rel))
	if err != nil {
		return
	}
	b.stamps[rel] = fileStamp{size: info.Size(), mod: info.ModTime()}
}

// changedUnderneath reports whether a file Klaudia wrote has since been changed
// by something else.
//
// This is the case §16 calls "manual edits made while Klaudia is working are
// reconciled". Klaudia cannot merge them, but it can refuse to pretend they are
// not there — which is what matters when the next step is an undo.
func (b *baseline) changedUnderneath(dir, rel string) bool {
	want, ok := b.stamps[rel]
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, rel))
	if err != nil {
		return true // deleted or moved out from under us
	}
	return info.Size() != want.size || !info.ModTime().Equal(want.mod)
}

// classify splits the working tree into who changed what.
func (m *Model) classify(status string) []ownedFile {
	var out []ownedFile
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 3 {
			continue
		}
		code := line[:2]
		path := unquoteStatusPath(strings.TrimSpace(line[2:]))
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		f := ownedFile{Path: path, Status: code}
		switch {
		case !m.touched[path]:
			f.Owner = ownerUser
		case m.base != nil && (m.base.dirty[path] || m.base.changedUnderneath(m.sess.CWD, path)):
			// Klaudia wrote it, but it was already dirty or has moved since.
			// Calling this "Klaudia's" would licence an undo that discards the
			// user's part of it.
			f.Owner = ownerBoth
		default:
			f.Owner = ownerKlaudia
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// changesCommand renders /changes.
func (m *Model) changesCommand() {
	if m.sess == nil || m.sess.CWD == "" {
		m.appendLine(errStyle.Render("no working directory"))
		return
	}
	status, err := gitOutput(m.sess.CWD, "status", "--porcelain")
	if err != nil {
		m.appendLine(errStyle.Render("git: " + strings.TrimSpace(status)))
		return
	}
	files := m.classify(status)
	if len(files) == 0 {
		m.appendLine(bannerStyle.Render("Working tree clean."))
		return
	}
	m.appendLine(bannerStyle.Render(renderOwnership(files)))
}

// renderOwnership groups the working tree by who changed what.
func renderOwnership(files []ownedFile) string {
	var b strings.Builder
	b.WriteString("Working tree\n")

	section := func(title string, owner fileOwner, note string) {
		var group []ownedFile
		for _, f := range files {
			if f.Owner == owner {
				group = append(group, f)
			}
		}
		if len(group) == 0 {
			return
		}
		b.WriteString("\n" + title + "\n")
		for _, f := range group {
			fmt.Fprintf(&b, "  %s %s\n", strings.TrimSpace(f.Status), f.Path)
		}
		if note != "" {
			b.WriteString("  " + note + "\n")
		}
	}

	section("Your existing changes", ownerUser, "")
	section("Klaudia", ownerKlaudia, "")
	section("Both", ownerBoth,
		"changed by Klaudia and by you — undo will not touch these")

	return strings.TrimRight(b.String(), "\n")
}

// klaudiaOwned returns the paths safe to undo: written by Klaudia and not
// touched by anyone else.
func klaudiaOwned(files []ownedFile) []string {
	var out []string
	for _, f := range files {
		if f.Owner == ownerKlaudia {
			out = append(out, f.Path)
		}
	}
	return out
}

// warnIfDirtyAtStart tells the user what was already in flight, once, at
// startup.
//
// §16's first checkbox. It matters most for the case where the user forgot: a
// half-finished refactor from yesterday, discovered at commit time, after
// Klaudia has been editing the same files all morning.
func (m *Model) warnIfDirtyAtStart() {
	if m.base == nil || len(m.base.dirty) == 0 {
		return
	}
	names := make([]string, 0, len(m.base.dirty))
	for p := range m.base.dirty {
		names = append(names, p)
	}
	sort.Strings(names)
	if len(names) > 6 {
		names = append(names[:6], fmt.Sprintf("… and %d more", len(m.base.dirty)-6))
	}
	m.appendLine(hintStyle.Render(fmt.Sprintf(
		"%d file(s) were already modified before this session: %s. /changes separates them from Klaudia's.",
		len(m.base.dirty), strings.Join(names, ", "))))
}
