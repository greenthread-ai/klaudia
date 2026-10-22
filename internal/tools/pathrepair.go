package tools

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Filenames that differ from what the caller typed only in which *kind* of
// space they contain are a standing macOS trap: every screenshot is named
// "Screenshot 2026-10-21 at 4.44.59<U+202F>pm.png", where the character before
// "pm" is a NARROW NO-BREAK SPACE. It is invisible in every listing, survives
// copy-paste out of Finder, and nothing that retypes the name — a model, a
// user, a log line — reproduces it. The open() then fails with ENOENT on a path
// that looks character-for-character right, which is close to unfixable from
// the error alone: the two real sessions this came from spent four tool calls
// and an `od -c` working out that the bytes were e2 80 af.
//
// NBSP (U+00A0) arrives the same way from Office and the web, and a trailing
// space in a name copied out of a web page is the same class of problem.

// spaceEquivalent reports whether a and b name the same thing once every
// Unicode space is treated as interchangeable. It is deliberately not a general
// fuzzy match: only the space class is normalised, so two genuinely different
// names never collide.
func spaceEquivalent(a, b string) bool {
	return normaliseSpaces(a) == normaliseSpaces(b)
}

// normaliseSpaces maps every space-class rune to a plain ASCII space. Go's
// unicode.IsSpace covers the Z category, so U+00A0, U+2007 and U+202F are all
// included.
func normaliseSpaces(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s)
}

// resolveExistingPath resolves p against the working directory and, when
// nothing is there, looks for a sibling whose name differs from the one asked
// for only in its space characters. The repaired path is returned only when
// exactly one candidate matches — an ambiguous directory is left to fail,
// because silently picking one of two files is worse than the ENOENT.
//
// Only for tools that require the file to already exist. Write must not use it:
// "create a file whose name nearly matches an existing one" is a legitimate
// request, and redirecting it would overwrite the wrong file.
func resolveExistingPath(tctx Context, p string) string {
	full := resolvePath(tctx, p)
	if full == "" {
		return full
	}
	if _, err := os.Lstat(full); err == nil {
		return full
	}
	if !hasSpace(full) {
		return full // nothing to normalise; don't pay for a directory read
	}
	repaired, ok := repairSpaces(full)
	if !ok {
		return full
	}
	return repaired
}

func hasSpace(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// repairSpaces rebuilds path one component at a time, replacing any component
// that does not exist with the unique existing sibling it is space-equivalent
// to. The walk is per-component because the mismatch can be in a directory name
// ("~/My Documents") as easily as in the leaf.
func repairSpaces(path string) (string, bool) {
	dir, name := filepath.Split(path)
	dir = filepath.Clean(dir)
	if name == "" || dir == path {
		return "", false
	}
	// Fix the ancestors first: a repaired parent is what makes the leaf
	// findable at all.
	if _, err := os.Lstat(dir); err != nil {
		fixedDir, ok := repairSpaces(dir)
		if !ok {
			return "", false
		}
		dir = fixedDir
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	match := ""
	for _, e := range entries {
		if e.Name() == name {
			return filepath.Join(dir, name), true // exact hit, after a parent repair
		}
		if !spaceEquivalent(e.Name(), name) {
			continue
		}
		if match != "" {
			return "", false // two candidates: refuse to guess
		}
		match = e.Name()
	}
	if match == "" {
		return "", false
	}
	return filepath.Join(dir, match), true
}
