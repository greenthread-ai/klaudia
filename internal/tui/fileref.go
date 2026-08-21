package tui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/greenthread-ai/klaudia/internal/native/search"
)

// File references: fuzzy @-completion, a cached path index, and understanding
// the path:line form that every compiler and stack trace prints.

// globFiles is indirected so tests can count and control index rebuilds.
var globFiles = func(root string) ([]string, error) {
	return search.Glob(search.GlobOptions{Root: root})
}

const (
	pathIndexTTL      = 30 * time.Second
	pathIndexMaxFiles = 50000
	recentPathsMax    = 32
)

// pathIndex caches the repo file list. Completion used to re-walk the entire
// tree on every Tab keystroke.
type pathIndex struct {
	root    string
	paths   []string
	builtAt time.Time
}

// files returns the cached listing, rebuilding when stale or when the root
// changed. now is injected so tests don't have to sleep.
func (p *pathIndex) files(root string, now time.Time) []string {
	if p.root == root && !p.builtAt.IsZero() && now.Sub(p.builtAt) < pathIndexTTL {
		return p.paths
	}
	paths, err := globFiles(root)
	if err != nil {
		return p.paths // keep whatever we had rather than losing completion
	}
	if len(paths) > pathIndexMaxFiles {
		paths = paths[:pathIndexMaxFiles] // search.Glob is mtime-ordered, so this keeps the freshest
	}
	p.root, p.paths, p.builtAt = root, paths, now
	return p.paths
}

func (p *pathIndex) invalidate() { p.builtAt = time.Time{} }

// noteRecentPath records a file Klaudia touched, so completion can rank it
// first. "The file we just edited" beats "the file with the newest mtime", and
// it also makes a brand-new file completable before the index expires.
func (m *Model) noteRecentPath(path string) {
	if path == "" {
		return
	}
	if root := m.rootDir(); root != "" {
		if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
			path = rel
		}
	}
	for i, p := range m.recentPaths {
		if p == path {
			m.recentPaths = append(m.recentPaths[:i], m.recentPaths[i+1:]...)
			break
		}
	}
	m.recentPaths = append([]string{path}, m.recentPaths...)
	if len(m.recentPaths) > recentPathsMax {
		m.recentPaths = m.recentPaths[:recentPathsMax]
	}
}

func (m *Model) rootDir() string {
	if m.sess != nil && m.sess.CWD != "" {
		return m.sess.CWD
	}
	return "."
}

// fuzzyScore scores candidate against pattern, reporting whether it matches at
// all. Higher is better. The tiers deliberately keep a plain prefix match on top
// so existing muscle memory still lands where it used to; fuzzy matching only
// adds candidates below that.
func fuzzyScore(pattern, candidate string) (int, bool) {
	if pattern == "" {
		return 1, true
	}
	// Smart case: an uppercase rune in the pattern makes matching case-sensitive.
	pat, cand := pattern, candidate
	if !hasUpper(pattern) {
		pat, cand = strings.ToLower(pattern), strings.ToLower(candidate)
	}
	base := cand
	if i := strings.LastIndexByte(cand, '/'); i >= 0 {
		base = cand[i+1:]
	}

	switch {
	case base == pat:
		return 3000 - len(cand), true
	case strings.HasPrefix(cand, pat):
		return 2000 - len(cand), true
	case strings.HasPrefix(base, pat):
		return 1500 - len(cand), true
	case strings.Contains(base, pat):
		return 1000 - len(cand), true
	case strings.Contains(cand, pat):
		return 600 - len(cand), true
	}
	score, ok := subsequenceScore(pat, cand)
	if !ok {
		return 0, false
	}
	return score - len(cand), true
}

// subsequenceScore matches pattern as an ordered subsequence, rewarding
// consecutive runs and matches at segment boundaries.
func subsequenceScore(pat, cand string) (int, bool) {
	score, pi, prev := 200, 0, -2
	for ci := 0; ci < len(cand) && pi < len(pat); ci++ {
		if cand[ci] != pat[pi] {
			continue
		}
		if ci == prev+1 {
			score += 10 // consecutive
		}
		if ci == 0 || cand[ci-1] == '/' || cand[ci-1] == '.' || cand[ci-1] == '_' || cand[ci-1] == '-' {
			score += 15 // segment boundary
		}
		prev = ci
		pi++
	}
	if pi < len(pat) {
		return 0, false
	}
	return score, true
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// fileRef is a path with an optional line and column, as printed by compilers,
// linters and stack traces.
type fileRef struct {
	Path string
	Line int
	Col  int
}

func (f fileRef) String() string {
	switch {
	case f.Line > 0 && f.Col > 0:
		return f.Path + ":" + strconv.Itoa(f.Line) + ":" + strconv.Itoa(f.Col)
	case f.Line > 0:
		return f.Path + ":" + strconv.Itoa(f.Line)
	}
	return f.Path
}

// splitLineSuffix peels a trailing :line[:col] off s. It does not check that the
// path exists, so it is safe to use while the user is still typing.
func splitLineSuffix(s string) (path string, line, col int) {
	path = s
	for i := 0; i < 2; i++ {
		idx := strings.LastIndexByte(path, ':')
		if idx <= 0 || idx == len(path)-1 {
			break
		}
		n, err := strconv.Atoi(path[idx+1:])
		if err != nil || n <= 0 {
			break
		}
		path = path[:idx]
		if col == 0 && line != 0 {
			col = line
		}
		line = n
	}
	return path, line, col
}

// parseFileRef recognises a "path:line:col" reference and confirms the file
// exists. Existence is the disambiguator: without it, "10:30" in prose and the
// port in "https://host:443/x" would both look like references.
func parseFileRef(root, s string, extraDirs []string) (fileRef, bool) {
	s = strings.Trim(s, "()[]<>,;\"'`")
	s = strings.TrimSuffix(s, ".")
	if s == "" || strings.Contains(s, "://") {
		return fileRef{}, false
	}
	path, line, col := splitLineSuffix(s)
	if path == "" {
		return fileRef{}, false
	}
	roots := append([]string{root}, extraDirs...)
	if filepath.IsAbs(path) {
		roots = []string{""}
	}
	for _, r := range roots {
		full := path
		if r != "" {
			full = filepath.Join(r, path)
		}
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return fileRef{Path: path, Line: line, Col: col}, true
		}
	}
	return fileRef{}, false
}
