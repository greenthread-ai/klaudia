package main

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is where README.md and go.mod live, relative to this package's
// directory (go test runs each package in its own source directory).
const repoRoot = "../.."

// envNamePattern matches a string literal that is, in its entirety, the name of
// one of Klaudia's environment variables. A literal that merely mentions one —
// "Set KLAUDIA_MAX_RETRIES to retry more." — is prose, not a read, and does not
// match.
var envNamePattern = regexp.MustCompile(`^(KLAUDIA|ANTHROPIC)_[A-Z0-9_]+$`)

// undocumentedEnvNames are literals that match envNamePattern but are not
// settings a user can set — for example a marker Klaudia puts in its own
// children's environment to recognise them later. Add one here only with a
// comment saying why a user never needs to know about it.
var undocumentedEnvNames = map[string]bool{}

// TestREADMEDocumentsEveryEnvVar keeps the README's "Environment variables"
// table in step with the code. It collects every string literal in non-test Go
// source that is exactly a KLAUDIA_* or ANTHROPIC_* name — whatever reads it:
// os.Getenv, os.LookupEnv, or a helper such as the browser package's getenv
// and envBool — and fails if the table has no row for one, or has a row for a
// name no code mentions any more.
func TestREADMEDocumentsEveryEnvVar(t *testing.T) {
	inCode := envNamesInSource(t, repoRoot)
	if len(inCode) == 0 {
		t.Fatal("found no KLAUDIA_*/ANTHROPIC_* literals in the source; the scan is broken")
	}
	documented := envNamesInREADME(t, filepath.Join(repoRoot, "README.md"))

	for _, name := range sortedKeys(inCode) {
		if undocumentedEnvNames[name] || documented[name] {
			continue
		}
		t.Errorf("%s is read at %s but has no row in README.md's \"Environment variables\" table",
			name, strings.Join(inCode[name], ", "))
	}
	for _, name := range sortedKeys(documented) {
		if _, ok := inCode[name]; !ok {
			t.Errorf("README.md's \"Environment variables\" table lists %s, but no non-test Go source names it", name)
		}
	}
}

// envNamesInSource maps each matching literal to the file:line positions it
// appears at, across every non-test .go file under root. Hidden directories
// (.git, and .claude, whose worktrees hold other checkouts of this repository),
// testdata and vendor are skipped.
func envNamesInSource(t *testing.T, root string) map[string][]string {
	t.Helper()
	found := map[string][]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || !envNamePattern.MatchString(s) {
				return true
			}
			pos := fset.Position(lit.Pos())
			rel, _ := filepath.Rel(root, pos.Filename)
			found[s] = append(found[s], filepath.ToSlash(rel)+":"+strconv.Itoa(pos.Line))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scanning Go source: %v", err)
	}
	return found
}

// envNamesInREADME returns the variable names in the first column of the table
// under the README's "## Environment variables" heading.
func envNamesInREADME(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening README: %v", err)
	}
	defer f.Close()

	names := map[string]bool{}
	inSection := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			if inSection {
				break
			}
			inSection = strings.TrimSpace(line) == "## Environment variables"
			continue
		}
		if !inSection || !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		name := strings.Trim(strings.TrimSpace(cells[1]), "`")
		if envNamePattern.MatchString(name) {
			names[name] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading README: %v", err)
	}
	if !inSection {
		t.Fatal(`README.md has no "## Environment variables" section`)
	}
	if len(names) == 0 {
		t.Fatal(`README.md's "Environment variables" section has no table rows`)
	}
	return names
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
