package search

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeTree creates files (relative path -> content) under dir. A path ending
// in "/" creates an empty directory.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if rel[len(rel)-1] == '/' {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// globRel runs Glob and returns the results relative to root, sorted.
func globRel(t *testing.T, root, pattern string) []string {
	t.Helper()
	got, err := Glob(GlobOptions{Root: root, Pattern: pattern})
	if err != nil {
		t.Fatal(err)
	}
	return relSorted(t, root, got)
}

func relSorted(t *testing.T, root string, paths []string) []string {
	t.Helper()
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	slices.Sort(out)
	return out
}

func assertPaths(t *testing.T, got []string, want ...string) {
	t.Helper()
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGlobHonoursGitignore(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".git/":            "",
		".gitignore":       "# build output\ntarget/\ncoverage/\n*.log\n!keep.log\n/rootonly.txt\n",
		"main.go":          "",
		"target/a.go":      "",
		"coverage/c.out":   "",
		"sub/x.log":        "",
		"sub/keep.log":     "",
		"rootonly.txt":     "",
		"sub/rootonly.txt": "",
		"sub/.gitignore":   "gen/\n",
		"sub/gen/g.go":     "",
		"gen/g.go":         "", // sub's rule does not reach the root
	})
	assertPaths(t, globRel(t, dir, ""),
		"main.go", "sub/keep.log", "sub/rootonly.txt", "gen/g.go")
}

func TestGrepHonoursGitignore(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".git/":       "",
		".gitignore":  "dist\n",
		"src/a.js":    "needle\n",
		"dist/a.js":   "needle\n",
		"dist/b.js":   "needle\n",
		"src/dist/x":  "needle\n", // unanchored: ignored at any depth
		"src/distx/y": "needle\n",
	})
	m, err := Grep(GrepOptions{Pattern: "needle", Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, hit := range m {
		files = append(files, hit.File)
	}
	assertPaths(t, relSorted(t, dir, files), "src/a.js", "src/distx/y")
}

func TestGitignoreNeedsARepository(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".gitignore": "*.txt\n",
		".ignore":    "*.md\n",
		"a.txt":      "",
		"b.md":       "",
	})
	// Outside a repository .gitignore means nothing (as for ripgrep and git);
	// .ignore applies anywhere.
	assertPaths(t, globRel(t, dir, ""), "a.txt")
}

func TestIgnoreFilesAboveTheRootApply(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".git/info/exclude":  "*.secret\n",
		".gitignore":         "*.tmp\n",
		"pkg/.ignore":        "*.bak\n",
		"pkg/inner/a.go":     "",
		"pkg/inner/a.tmp":    "",
		"pkg/inner/a.bak":    "",
		"pkg/inner/a.secret": "",
	})
	assertPaths(t, globRel(t, filepath.Join(dir, "pkg", "inner"), ""), "a.go")
}

func TestIgnoreBeatsGitignoreAndLaterLinesWin(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".git/":         "",
		".gitignore":    "*.gen\nlogs/**\n!logs/keep.txt\n",
		".ignore":       "!keep.gen\n",
		"a.gen":         "",
		"keep.gen":      "",
		"logs/drop.txt": "",
		"logs/keep.txt": "",
	})
	assertPaths(t, globRel(t, dir, ""), "keep.gen", "logs/keep.txt")
}

func TestNamedPathsBypassFilters(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".git/":                     "",
		".gitignore":                "dist/\n.env\n",
		"dist/app.js":               "",
		"src/app.js":                "",
		".env":                      "",
		"node_modules/pkg/index.js": "",
	})
	assertPaths(t, globRel(t, dir, "**/*.js"), "src/app.js")
	// A pattern whose leading segments name an ignored directory walks it.
	assertPaths(t, globRel(t, dir, "dist/*.js"), "dist/app.js")
	assertPaths(t, globRel(t, dir, "node_modules/**/*.js"), "node_modules/pkg/index.js")
	// So does a search rooted inside it.
	assertPaths(t, globRel(t, filepath.Join(dir, "dist"), "*.js"), "app.js")
	// A pattern that is exactly an ignored file's path finds it.
	assertPaths(t, globRel(t, dir, ".env"), ".env")
}

func TestHiddenEntriesWhenNamed(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".git/":                         "",
		".github/workflows/ci.yml":      "",
		".github/workflows/.hidden.yml": "",
		"config.yml":                    "",
		".eslintrc":                     "x\n",
		"sub/.eslintrc":                 "x\n",
		".env.example":                  "",
		".venv/lib/site.py":             "",
		"tool.py":                       "",
		"a/.config/settings.json":       "",
	})
	// Unnamed hidden entries stay out.
	assertPaths(t, globRel(t, dir, "**/*.yml"), "config.yml")
	assertPaths(t, globRel(t, dir, "**/*.py"), "tool.py")
	assertPaths(t, globRel(t, dir, ""), "config.yml", "tool.py")
	// Named ones come in.
	assertPaths(t, globRel(t, dir, ".github/**/*.yml"), ".github/workflows/ci.yml")
	assertPaths(t, globRel(t, dir, ".eslintrc"), ".eslintrc", "sub/.eslintrc")
	assertPaths(t, globRel(t, dir, ".env*"), ".env.example")
	assertPaths(t, globRel(t, dir, "**/.config/*.json"), "a/.config/settings.json")

	m, err := Grep(GrepOptions{Pattern: "x", Root: dir, Glob: ".eslintrc"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 2 {
		t.Errorf("Grep with glob .eslintrc: got %+v, want both .eslintrc files", m)
	}
	// Hidden still admits everything but the skip list.
	all, err := Glob(GlobOptions{Root: dir, Hidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 10-1 { // every file but .git/
		t.Errorf("Hidden: got %d files %v", len(all), relSorted(t, dir, all))
	}
}

func TestParseIgnore(t *testing.T) {
	rules := parseIgnore("/r", []byte("# comment\n\n\\#hash\nspace\\ \ntrail   \r\n!neg\n/anch\nd/\na/b\n{x}\n/\n"))
	type want struct {
		pattern                   string
		anchored, dirOnly, negate bool
	}
	wants := []want{
		{`\#hash`, false, false, false},
		{`space\ `, false, false, false},
		{"trail", false, false, false},
		{"neg", false, false, true},
		{"anch", true, false, false},
		{"d", false, true, false},
		{"a/b", true, false, false},
		{`\{x\}`, false, false, false},
	}
	if len(rules) != len(wants) {
		t.Fatalf("got %d rules %+v, want %d", len(rules), rules, len(wants))
	}
	for i, w := range wants {
		r := rules[i]
		if r.pattern != w.pattern || r.anchored != w.anchored || r.dirOnly != w.dirOnly || r.negate != w.negate || r.base != "/r/" {
			t.Errorf("rule %d: got %+v, want %+v", i, r, w)
		}
	}
	if !ignored(rules, "/r/#hash", "#hash", false) || !ignored(rules, "/r/x/{x}", "{x}", false) {
		t.Error("escaped # and literal braces should match")
	}
	if ignored(rules, "/r/x/anch", "anch", false) || !ignored(rules, "/r/anch", "anch", false) {
		t.Error("a leading slash anchors to the ignore file's directory")
	}
	if ignored(rules, "/r/d", "d", false) || !ignored(rules, "/r/x/d", "d", true) {
		t.Error("a trailing slash matches directories only")
	}
}
