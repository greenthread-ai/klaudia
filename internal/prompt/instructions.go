package prompt

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Project instructions come from files people keep for their agents, and the
// shapes they use are not Klaudia's invention: a CLAUDE.md that is just
// "@AGENTS.md", a workspace-level CLAUDE.md above several checkouts, an
// AGENTS.md in a repo that has no CLAUDE.md, shared rules in .claude/rules.
// Klaudia used to read three fixed paths — ~/.claude/CLAUDE.md, the git
// root's CLAUDE.md and the working directory's — so each of those shapes
// reached the model as a literal "@AGENTS.md" line or not at all.

const (
	maxImportDepth = 5
	maxInstrBytes  = 256 << 10 // per file; a runaway file is not instructions
)

// instructionFile is one loaded file and the text it contributes.
type instructionFile struct {
	path string
	text string
}

// loadProjectInstructions gathers instructions for a session in cwd, farthest
// first so the closest have the last word:
//
//  1. the user's own: ~/.claude/CLAUDE.md and ~/.claude/rules/*.md;
//  2. for each directory from the filesystem root down to cwd: its CLAUDE.md,
//     or AGENTS.md when it has no CLAUDE.md, then its .claude/rules/*.md.
//
// In each file, a line that is only "@path" is replaced by that file's
// contents (relative to the importing file; "~/" is home), to a depth of
// five; an "@path" inside a line is left as written and the file it names is
// appended after it. HTML comments are removed. A file is included once
// however many ways it is reached.
func loadProjectInstructions(cwd string) string {
	l := &instrLoader{seen: map[string]bool{}}
	if home, err := os.UserHomeDir(); err == nil {
		l.home = home
		l.add(filepath.Join(home, ".claude", "CLAUDE.md"), 0)
		l.addRules(filepath.Join(home, ".claude", "rules"))
	}
	for _, dir := range ancestors(cwd) {
		if !l.add(filepath.Join(dir, "CLAUDE.md"), 0) {
			l.add(filepath.Join(dir, "AGENTS.md"), 0)
		}
		l.addRules(filepath.Join(dir, ".claude", "rules"))
	}
	parts := make([]string, 0, len(l.files))
	for _, f := range l.files {
		parts = append(parts, "Contents of "+f.path+":\n\n"+f.text)
	}
	return strings.Join(parts, "\n\n")
}

// ancestors returns dir and every directory above it, root first.
func ancestors(dir string) []string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return []string{dir}
	}
	var out []string
	for d := abs; ; d = filepath.Dir(d) {
		out = append(out, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

type instrLoader struct {
	home  string
	seen  map[string]bool
	files []instructionFile
}

// add loads path (and what it imports) and reports whether the file exists.
// A file already loaded counts as existing, so a CLAUDE.md reached twice
// still suppresses the AGENTS.md fallback.
func (l *instrLoader) add(path string, depth int) bool {
	text, key, ok := l.read(path)
	if !ok {
		return false
	}
	if l.seen[key] {
		return true
	}
	l.seen[key] = true
	body := l.expand(text, filepath.Dir(path), depth)
	if strings.TrimSpace(body) != "" {
		l.files = append(l.files, instructionFile{path: path, text: strings.TrimSpace(body)})
	}
	return true
}

func (l *instrLoader) addRules(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Strings(matches)
	for _, m := range matches {
		l.add(m, 0)
	}
}

// read returns a file's text and its canonical path, or ok=false when it is
// missing, not a regular file, or too large.
func (l *instrLoader) read(path string) (text, key string, ok bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", false
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxInstrBytes {
		return "", "", false
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return "", "", false
	}
	return string(data), real, true
}

var (
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	importToken = regexp.MustCompile(`(?:^|\s)@(~?[A-Za-z0-9_./-]*[A-Za-z0-9_-][A-Za-z0-9_./-]*)`)
)

// expand strips comments and resolves imports in text read from dir. Lines in
// fenced code blocks are left alone: "@" there is code, not an import.
func (l *instrLoader) expand(text, dir string, depth int) string {
	text = htmlComment.ReplaceAllString(text, "")
	var out []string
	var trailing []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			out = append(out, line)
			continue
		}
		if inFence || depth >= maxImportDepth {
			out = append(out, line)
			continue
		}
		if m := importToken.FindStringSubmatch(trimmed); m != nil && "@"+m[1] == trimmed {
			if body, ok := l.importFile(m[1], dir, depth); ok {
				if body != "" {
					out = append(out, body)
				}
				continue
			}
		} else {
			for _, m := range importToken.FindAllStringSubmatch(line, -1) {
				if body, ok := l.importFile(m[1], dir, depth); ok && body != "" {
					trailing = append(trailing, body)
				}
			}
		}
		out = append(out, line)
	}
	return strings.Join(append(out, trailing...), "\n")
}

// importFile resolves an @path reference and returns the imported text. ok
// is false when the reference does not name a readable file — an "@mention"
// or an email address — and the text is then left as written. A file already
// loaded imports as nothing, which also ends cycles.
func (l *instrLoader) importFile(ref, dir string, depth int) (string, bool) {
	p := ref
	if strings.HasPrefix(p, "~/") {
		if l.home == "" {
			return "", false
		}
		p = filepath.Join(l.home, p[2:])
	} else if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	text, key, ok := l.read(p)
	if !ok {
		return "", false
	}
	if l.seen[key] {
		return "", true
	}
	l.seen[key] = true
	return strings.TrimSpace(l.expand(text, filepath.Dir(p), depth+1)), true
}
