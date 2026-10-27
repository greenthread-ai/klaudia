package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// Paths on screen are written the way a person would: relative to the
// working directory, or under ~ — never as the absolute path a tool reports.
//
// Seen in the TUI audit: Glob results, "Edited /…/main.go", a missing-file
// error, each two rows of /Users/…/Projects/… for a three-word fact. This is
// display only. The model keeps the absolute paths it was given, /last shows
// the output verbatim, and the OSC 8 linker resolves a relative path against
// the same working directory, so the shorter form is still clickable.

// displayPaths shortens every absolute path in s that sits under the working
// directory or the home directory.
func (m *Model) displayPaths(s string) string {
	if m.sess == nil || m.sess.CWD == "" || !strings.Contains(s, "/") {
		return s
	}
	for _, root := range m.pathRoots() {
		s = replaceRoot(s, root[0], root[1])
	}
	return s
}

// pathRoots returns (prefix, replacement) pairs, most specific first. The
// working directory is listed both as given and with symlinks resolved, since
// tools report whichever form they computed (/tmp and /private/tmp on macOS).
func (m *Model) pathRoots() [][2]string {
	var roots [][2]string
	add := func(dir, repl string) {
		if dir == "" || dir == "/" {
			return
		}
		roots = append(roots, [2]string{filepath.Clean(dir) + "/", repl})
	}
	add(m.sess.CWD, "")
	if real, err := filepath.EvalSymlinks(m.sess.CWD); err == nil && real != m.sess.CWD {
		add(real, "")
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(home, "~/")
	}
	return roots
}

// replaceRoot rewrites prefix to repl wherever prefix starts a path: at the
// start of the text or after a character that cannot be part of one. Without
// that boundary, a working directory of /a/b would turn /x/a/b/c into /xc.
func replaceRoot(s, prefix, repl string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		if i > 0 && isPathByte(s[i-1]) || i > 0 && s[i-1] == '.' {
			b.WriteString(s[:i+len(prefix)])
		} else {
			b.WriteString(s[:i])
			b.WriteString(repl)
		}
		s = s[i+len(prefix):]
	}
}

// tildePath writes a directory under the home directory as ~/…, for the places
// that show a directory itself rather than a path inside the working one.
func tildePath(dir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return dir
	}
	if dir == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(dir, filepath.Clean(home)+"/"); ok {
		return "~/" + rest
	}
	return dir
}
