// Package search provides in-process file globbing and content search,
// absorbing what the standalone tools/search (ripgrep replacement) binary did.
// The Glob and Grep tools build on these primitives.
package search

import (
	"bufio"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ignoredDirs are skipped during traversal (matches the JS/ripgrep defaults).
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true,
	".svn": true, ".hg": true, "vendor": true,
}

// shouldSkipDir reports whether a directory should be pruned from traversal.
func shouldSkipDir(name string, hidden bool) bool {
	if ignoredDirs[name] {
		return true
	}
	if !hidden && strings.HasPrefix(name, ".") {
		return true
	}
	return false
}

// GlobOptions configures Glob.
type GlobOptions struct {
	Root    string // base directory to search (defaults to ".")
	Pattern string // glob pattern, e.g. "**/*.go"; empty means all files
	Hidden  bool   // include dotfiles/dotdirs
}

// Glob returns files under Root matching Pattern, sorted by modification time
// (newest first) — matching the JS Glob tool's ordering.
func Glob(opts GlobOptions) ([]string, error) {
	root := opts.Root
	if root == "" {
		root = "."
	}
	type ent struct {
		path string
		mod  int64
	}
	var out []ent

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(d.Name(), opts.Hidden) {
				return filepath.SkipDir
			}
			return nil
		}
		if !opts.Hidden && strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if opts.Pattern != "" && !matchGlob(root, path, opts.Pattern) {
			return nil
		}
		info, ierr := d.Info()
		var mod int64
		if ierr == nil {
			mod = info.ModTime().UnixNano()
		}
		out = append(out, ent{path: path, mod: mod})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].mod > out[j].mod })
	paths := make([]string, len(out))
	for i, e := range out {
		paths[i] = e.path
	}
	return paths, nil
}

// matchGlob matches pattern against both the path relative to root and the
// basename, supporting "**" via doublestar.
func matchGlob(root, path, pattern string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	if ok, _ := doublestar.Match(pattern, rel); ok {
		return true
	}
	if ok, _ := doublestar.Match(pattern, filepath.Base(path)); ok {
		return true
	}
	return false
}

// GrepOptions configures Grep.
type GrepOptions struct {
	Pattern    string // regular expression
	Root       string // search root (file or directory)
	IgnoreCase bool
	Multiline  bool   // '.' matches newlines; pattern may span lines
	Glob       string // optional file filter (e.g. "*.go")
	Hidden     bool
	// Exts keeps only files with one of these extensions (".go", ".py"),
	// for a file-type filter. Empty means every file.
	Exts []string
	// Before and After ask for that many lines of context around each
	// matching line. Ignored in multiline mode.
	Before, After int
}

// GrepMatch is one matching line, or one line of context around a match.
type GrepMatch struct {
	File string
	// Line is 1-indexed. In multiline mode it is the line the match starts on.
	Line int
	// Text is the line, or in multiline mode the whole matched text.
	Text string
	// Context marks a line included only because it is near a match.
	Context bool
}

// Grep searches file contents for Pattern and returns matching lines.
func Grep(opts GrepOptions) ([]GrepMatch, error) {
	re, err := compile(opts)
	if err != nil {
		return nil, err
	}
	root := opts.Root
	if root == "" {
		root = "."
	}

	var matches []GrepMatch
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	visit := func(path string) {
		data, rerr := os.ReadFile(path)
		if rerr != nil || isBinary(data) {
			return
		}
		if opts.Multiline {
			// One entry per match, carrying where it starts and what it
			// matched: a bare "this file matches" was useless in content mode.
			for _, loc := range re.FindAllIndex(data, -1) {
				line := 1 + bytes.Count(data[:loc[0]], []byte{'\n'})
				matches = append(matches, GrepMatch{File: path, Line: line, Text: string(data[loc[0]:loc[1]])})
			}
			return
		}
		sc := bufio.NewScanner(bytes.NewReader(data))
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		if opts.Before <= 0 && opts.After <= 0 {
			n := 0
			for sc.Scan() {
				n++
				line := sc.Text()
				if re.MatchString(line) {
					matches = append(matches, GrepMatch{File: path, Line: n, Text: line})
				}
			}
			return
		}
		var lines []string
		for sc.Scan() {
			lines = append(lines, sc.Text())
		}
		matches = append(matches, withContext(path, lines, re, opts.Before, opts.After)...)
	}

	if !info.IsDir() {
		visit(root)
		return matches, nil
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldSkipDir(d.Name(), opts.Hidden) {
				return filepath.SkipDir
			}
			return nil
		}
		if !opts.Hidden && strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if opts.Glob != "" && !matchGlob(root, path, opts.Glob) {
			return nil
		}
		if len(opts.Exts) > 0 && !hasExt(path, opts.Exts) {
			return nil
		}
		visit(path)
		return nil
	})
	return matches, walkErr
}

// withContext returns the matching lines of one file with up to before/after
// lines around each, every line once and in order. Context lines are marked so
// a formatter can tell them apart.
func withContext(path string, lines []string, re *regexp.Regexp, before, after int) []GrepMatch {
	var out []GrepMatch
	next := 0 // first line index not yet emitted
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		start := max(i-before, next)
		for j := start; j < i; j++ {
			out = append(out, GrepMatch{File: path, Line: j + 1, Text: lines[j], Context: true})
		}
		if i >= next {
			out = append(out, GrepMatch{File: path, Line: i + 1, Text: line})
		} else {
			// Already emitted as trailing context of the previous match;
			// it is a match in its own right.
			for k := len(out) - 1; k >= 0; k-- {
				if out[k].Line == i+1 {
					out[k].Context = false
					break
				}
			}
		}
		end := min(i+after, len(lines)-1)
		for j := max(i+1, next); j <= end; j++ {
			out = append(out, GrepMatch{File: path, Line: j + 1, Text: lines[j], Context: true})
		}
		next = max(next, end+1, i+1)
	}
	return out
}

// hasExt reports whether path ends in one of exts (case-insensitive).
func hasExt(path string, exts []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range exts {
		if ext == e {
			return true
		}
	}
	return false
}

// compile builds the regexp from the options, applying case-insensitive and
// dot-matches-newline flags as requested.
func compile(opts GrepOptions) (*regexp.Regexp, error) {
	pat := opts.Pattern
	var flags string
	if opts.IgnoreCase {
		flags += "i"
	}
	if opts.Multiline {
		flags += "s" // dot matches newline
	}
	if flags != "" {
		pat = "(?" + flags + ")" + pat
	}
	return regexp.Compile(pat)
}

// isBinary heuristically detects binary content (presence of a NUL byte in the
// first 8KB), matching ripgrep's default of skipping binary files.
func isBinary(data []byte) bool {
	n := min(len(data), 8192)
	return bytes.IndexByte(data[:n], 0) >= 0
}
