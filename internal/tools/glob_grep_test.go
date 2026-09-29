package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// searchTree lays out a small project for Glob and Grep:
//
//	a.go        "package a\nfunc Hello() {}\n"
//	b.go        "package b\n// hello again\nfunc hello() {}\n"
//	sub/c.txt   "HELLO text\n"
//	.hidden.go  "hello hidden\n"
func searchTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"a.go":       "package a\nfunc Hello() {}\n",
		"b.go":       "package b\n// hello again\nfunc hello() {}\n",
		"sub/c.txt":  "HELLO text\n",
		".hidden.go": "hello hidden\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runTool(t *testing.T, tool Tool, tctx Context, in any) Result {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	res, err := tool.Execute(context.Background(), tctx, raw)
	if err != nil {
		t.Fatalf("%s: Execute error: %v", tool.Name(), err)
	}
	if len(res) != 1 {
		t.Fatalf("%s: got %d results, want 1", tool.Name(), len(res))
	}
	return res[0]
}

func TestGlobMatchesNewestFirstAndSkipsHidden(t *testing.T) {
	dir := searchTree(t)
	// Make a.go unambiguously the newest so the order is observable.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "b.go"), old, old); err != nil {
		t.Fatal(err)
	}
	g, err := NewGlob()
	if err != nil {
		t.Fatal(err)
	}
	res := runTool(t, g, Context{}, GlobInput{Pattern: "*.go", Path: dir})
	if res.IsError {
		t.Fatalf("glob failed: %+v", res)
	}
	want := filepath.Join(dir, "a.go") + "\n" + filepath.Join(dir, "b.go")
	if res.Content != want {
		t.Errorf("content = %q, want %q (newest first, dotfiles skipped)", res.Content, want)
	}
}

func TestGlobDefaultsToWorkingDir(t *testing.T) {
	dir := searchTree(t)
	g, _ := NewGlob()
	res := runTool(t, g, Context{WorkingDir: dir}, GlobInput{Pattern: "**/*.txt"})
	if res.Content != filepath.Join(dir, "sub", "c.txt") {
		t.Errorf("content = %q, want the one .txt under the working dir", res.Content)
	}
}

func TestGlobNoMatchesIsNotAnError(t *testing.T) {
	dir := searchTree(t)
	g, _ := NewGlob()
	res := runTool(t, g, Context{}, GlobInput{Pattern: "*.rs", Path: dir})
	if res.IsError || res.Content != "No files found" {
		t.Errorf("res = %+v, want a plain \"No files found\"", res)
	}
}

func TestGrepOutputModes(t *testing.T) {
	dir := searchTree(t)
	a, b, c := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go"), filepath.Join(dir, "sub", "c.txt")

	cases := []struct {
		name string
		in   GrepInput
		want string
	}{
		{
			name: "files_with_matches is the default and lists each file once",
			in:   GrepInput{Pattern: "hello", Glob: "*.go"},
			want: b,
		},
		{
			name: "ignore case widens the match",
			in:   GrepInput{Pattern: "hello", IgnoreCase: true, Glob: "*.go", OutputMode: "count"},
			want: a + ":1\n" + b + ":2",
		},
		{
			name: "content with line numbers",
			in:   GrepInput{Pattern: "hello", Glob: "*.go", OutputMode: "content", LineNum: true},
			want: b + ":2:// hello again\n" + b + ":3:func hello() {}",
		},
		{
			name: "content without line numbers",
			in:   GrepInput{Pattern: "^HELLO", OutputMode: "content"},
			want: c + ":HELLO text",
		},
		{
			name: "count is per file",
			in:   GrepInput{Pattern: "func", OutputMode: "count"},
			want: a + ":1\n" + b + ":1",
		},
	}
	g, err := NewGrep()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.Path = dir
			res := runTool(t, g, Context{}, tc.in)
			if res.IsError {
				t.Fatalf("grep failed: %+v", res)
			}
			if res.Content != tc.want {
				t.Errorf("content =\n%s\nwant\n%s", res.Content, tc.want)
			}
		})
	}
}

func TestGrepMultilineMatchesAcrossLines(t *testing.T) {
	dir := searchTree(t)
	g, _ := NewGrep()
	res := runTool(t, g, Context{WorkingDir: dir}, GrepInput{Pattern: `package b.*func hello`, Multiline: true})
	if res.Content != filepath.Join(dir, "b.go") {
		t.Errorf("content = %q, want b.go (the pattern spans its lines)", res.Content)
	}
}

func TestGrepSingleFileRoot(t *testing.T) {
	dir := searchTree(t)
	g, _ := NewGrep()
	a := filepath.Join(dir, "a.go")
	res := runTool(t, g, Context{}, GrepInput{Pattern: "Hello", Path: a, OutputMode: "content", LineNum: true})
	if res.Content != a+":2:func Hello() {}" {
		t.Errorf("content = %q", res.Content)
	}
}

func TestGrepNoMatchesAndErrors(t *testing.T) {
	dir := searchTree(t)
	g, _ := NewGrep()

	res := runTool(t, g, Context{WorkingDir: dir}, GrepInput{Pattern: "nothing-matches-this"})
	if res.IsError || res.Content != "No matches found" {
		t.Errorf("no-match res = %+v", res)
	}

	res = runTool(t, g, Context{WorkingDir: dir}, GrepInput{Pattern: "("})
	if !res.IsError || !strings.HasPrefix(res.Content, "Error:") {
		t.Errorf("bad regex res = %+v, want an error result", res)
	}

	res = runTool(t, g, Context{}, GrepInput{Pattern: "x", Path: filepath.Join(dir, "missing")})
	if !res.IsError {
		t.Errorf("missing root res = %+v, want an error result", res)
	}
}

func TestGlobAndGrepValidateAgainstSchema(t *testing.T) {
	g, _ := NewGlob()
	gr, _ := NewGrep()
	for _, tool := range []Tool{g, gr} {
		if err := tool.ValidateInput(json.RawMessage(`{"pattern":"x"}`)); err != nil {
			t.Errorf("%s: valid input rejected: %v", tool.Name(), err)
		}
		if err := tool.ValidateInput(json.RawMessage(`{}`)); err == nil {
			t.Errorf("%s: missing pattern accepted", tool.Name())
		}
	}
}
