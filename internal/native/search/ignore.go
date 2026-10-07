package search

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Traversal filtering for the Glob and Grep walks: the fixed skip list,
// hidden entries, and .gitignore / .ignore files, with ripgrep's semantics
// where they matter:
//
//   - .gitignore (and .git/info/exclude) applies only inside a git
//     repository; .ignore applies anywhere. Ignore files in the directories
//     between the repository root and the search root apply too.
//   - Precedence is git's: a deeper file beats a shallower one, .ignore beats
//     .gitignore in the same directory, and within a file the last matching
//     line wins. An ignored directory is pruned, so — as in git — a negation
//     cannot re-include a file beneath it.
//   - The search root itself is never filtered: naming a path searches it.
//     Likewise a pattern's literal leading segments (".github" in
//     ".github/**/*.yml", "dist" in "dist/*.js") are walked even when hidden,
//     ignored or on the skip list, and a pattern that spells out a dot-name
//     (".env*", "**/.eslintrc") admits hidden entries.
//
// Not implemented: the global core.excludesFile, and a nested repository
// resetting its parent's rules.

// ignoredDirs are skipped during traversal (matches the JS/ripgrep defaults)
// unless the search names them.
var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true,
	".svn": true, ".hg": true, "vendor": true,
}

// ignoreRule is one line of an ignore file.
type ignoreRule struct {
	base     string // directory holding the ignore file: absolute, slash-separated, trailing "/"
	pattern  string // doublestar pattern
	anchored bool   // matched against the path relative to base, not the basename
	dirOnly  bool   // trailing "/": directories only
	negate   bool   // leading "!"
}

// match reports whether the rule matches the entry at abs (absolute,
// slash-separated) named name.
func (r ignoreRule) match(abs, name string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	rel, ok := strings.CutPrefix(abs, r.base)
	if !ok || rel == "" {
		return false
	}
	if !r.anchored {
		ok, _ := doublestar.Match(r.pattern, name)
		return ok
	}
	// doublestar lets "dir/**" match "dir" itself; git does not, and the
	// difference matters when a later "!dir/keep" re-includes a child.
	if prefix, found := strings.CutSuffix(r.pattern, "/**"); found && rel == prefix {
		return false
	}
	ok, _ = doublestar.Match(r.pattern, rel)
	return ok
}

// parseIgnore reads gitignore-format lines whose rules are relative to base
// (absolute, slash-separated).
func parseIgnore(base string, data []byte) []ignoreRule {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	var rules []ignoreRule
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		// Trailing spaces are dropped unless escaped with a backslash.
		for strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "\\ ") {
			line = line[:len(line)-1]
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{base: base}
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimRight(line, "/")
		}
		if strings.HasPrefix(line, "/") {
			r.anchored = true
			line = strings.TrimLeft(line, "/")
		} else {
			r.anchored = strings.Contains(line, "/")
		}
		if line == "" {
			continue
		}
		r.pattern = escapeBraces(line)
		if !doublestar.ValidatePattern(r.pattern) {
			continue
		}
		rules = append(rules, r)
	}
	return rules
}

// escapeBraces makes "{" and "}" literal: they are alternation to doublestar
// but plain characters to git.
func escapeBraces(p string) string {
	if !strings.ContainsAny(p, "{}") {
		return p
	}
	var b strings.Builder
	escaped := false
	for _, c := range p {
		if !escaped && (c == '{' || c == '}') {
			b.WriteByte('\\')
		}
		escaped = !escaped && c == '\\'
		b.WriteRune(c)
	}
	return b.String()
}

// walkFilter decides which entries a Glob or Grep walk visits.
type walkFilter struct {
	root        string // the walk root, cleaned, as WalkDir reports it
	absRoot     string // absolute, slash-separated
	git         bool   // the root is inside a git repository
	hiddenDirs  bool   // descend into dot-directories
	hiddenFiles bool   // visit dotfiles
	named       []string
	rules       map[string][]ignoreRule // walked directory -> rules in effect inside it
}

// newWalkFilter prepares a filter for a walk of root (already cleaned) whose
// entries will be matched against pattern (may be empty).
func newWalkFilter(root string, hidden bool, pattern string) *walkFilter {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	w := &walkFilter{
		root:    root,
		absRoot: filepath.ToSlash(abs),
		rules:   map[string][]ignoreRule{},
	}
	w.named, w.hiddenDirs, w.hiddenFiles = patternNames(pattern, hidden)
	w.rules[parentKey] = w.ancestorRules(abs)
	return w
}

// parentKey stands for "the directory above the root" in the rules map.
const parentKey = "\x00parent"

// patternNames splits pattern into its literal leading directory segments and
// reports whether it spells out a dot-name for a directory or file.
func patternNames(pattern string, hidden bool) (named []string, hiddenDirs, hiddenFiles bool) {
	hiddenDirs, hiddenFiles = hidden, hidden
	if pattern == "" {
		return nil, hiddenDirs, hiddenFiles
	}
	segs := strings.Split(pattern, "/")
	literal := true
	for i, seg := range segs {
		last := i == len(segs)-1
		if literal && seg != "" && !strings.ContainsAny(seg, `*?[{\`) {
			named = append(named, seg)
		} else {
			literal = false
			// A dot-name after a wildcard ("**/.github/*.yml") can sit under
			// any directory, so dot-directories must be walked to find it.
			if !last && dotName(seg) {
				hiddenDirs = true
			}
		}
		// A dot-name in the final segment names files, and Glob also
		// matches a pattern against basenames at any depth, so admit
		// dotfiles everywhere even when the segment was literal (".env").
		if last && dotName(seg) {
			hiddenFiles = true
		}
	}
	return named, hiddenDirs, hiddenFiles
}

// dotName reports whether a pattern segment names a dot-entry, including as a
// brace alternative ("{.github,docs}").
func dotName(seg string) bool {
	if seg == "." || seg == ".." {
		return false
	}
	return strings.HasPrefix(seg, ".") || strings.Contains(seg, "{.") || strings.Contains(seg, ",.")
}

// isNamed reports whether rel (slash-separated, relative to the root) is one
// of the pattern's literal leading segments or a prefix of them.
func (w *walkFilter) isNamed(rel string) bool {
	segs := strings.Split(rel, "/")
	if len(segs) > len(w.named) {
		return false
	}
	for i, s := range segs {
		if s != w.named[i] {
			return false
		}
	}
	return true
}

// skip reports whether the walk should leave out p; for a directory, true
// means prune it. A directory that is kept has its ignore files loaded.
func (w *walkFilter) skip(p string, d fs.DirEntry) bool {
	isDir := d.IsDir()
	if p == w.root {
		if isDir {
			w.enter(p, w.absRoot, w.rules[parentKey])
		}
		return false
	}
	rel, err := filepath.Rel(w.root, p)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	abs := path.Join(w.absRoot, rel)
	name := d.Name()
	parent := filepath.Dir(p)
	if !w.isNamed(rel) {
		if isDir && ignoredDirs[name] {
			return true
		}
		if strings.HasPrefix(name, ".") && !(isDir && w.hiddenDirs || !isDir && w.hiddenFiles) {
			return true
		}
		if ignored(w.rules[parent], abs, name, isDir) {
			return true
		}
	}
	if isDir {
		w.enter(p, abs, w.rules[parent])
	}
	return false
}

// ignored applies rules (lowest precedence first): the last match decides.
func ignored(rules []ignoreRule, abs, name string, isDir bool) bool {
	for i := len(rules) - 1; i >= 0; i-- {
		if rules[i].match(abs, name, isDir) {
			return !rules[i].negate
		}
	}
	return false
}

// enter records the rules in effect inside directory p: the inherited ones
// followed by its own ignore files.
func (w *walkFilter) enter(p, abs string, inherited []ignoreRule) {
	own := w.load(abs)
	if len(own) == 0 {
		w.rules[p] = inherited
		return
	}
	rules := make([]ignoreRule, 0, len(inherited)+len(own))
	w.rules[p] = append(append(rules, inherited...), own...)
}

// load reads the ignore files in directory abs, .gitignore before .ignore so
// that .ignore takes precedence.
func (w *walkFilter) load(abs string) []ignoreRule {
	var rules []ignoreRule
	names := []string{".ignore"}
	if w.git {
		names = []string{".gitignore", ".ignore"}
	}
	for _, n := range names {
		if data, err := os.ReadFile(filepath.Join(filepath.FromSlash(abs), n)); err == nil {
			rules = append(rules, parseIgnore(abs, data)...)
		}
	}
	return rules
}

// ancestorRules finds the enclosing git repository (setting w.git) and
// returns the rules from .git/info/exclude and from the ignore files in the
// directories between the repository root and abs, exclusive of abs.
func (w *walkFilter) ancestorRules(abs string) []ignoreRule {
	var dirs []string // abs's ancestors, nearest first, up to the repo root
	gitRoot := ""
	for dir := abs; ; {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			gitRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
		dirs = append(dirs, dir)
	}
	if gitRoot == "" {
		return nil
	}
	w.git = true
	var rules []ignoreRule
	if data, err := os.ReadFile(filepath.Join(gitRoot, ".git", "info", "exclude")); err == nil {
		rules = parseIgnore(filepath.ToSlash(gitRoot), data)
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		rules = append(rules, w.load(filepath.ToSlash(dirs[i]))...)
	}
	return rules
}
