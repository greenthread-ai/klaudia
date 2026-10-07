package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGrep(t *testing.T, dir string, in map[string]any) (string, error) {
	t.Helper()
	g, err := NewGrep()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(in)
	if err := g.ValidateInput(raw); err != nil {
		return "", err
	}
	res, err := g.Execute(context.Background(), Context{WorkingDir: dir}, raw)
	if err != nil {
		t.Fatal(err)
	}
	return res[0].Content, nil
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGrepContextLines(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.txt": "one\ntwo\nHIT\nfour\nfive\nsix\nseven\nHIT\nnine\n"})
	out, err := runGrep(t, dir, map[string]any{"pattern": "HIT", "output_mode": "content", "-n": true, "-C": 1})
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "a.txt")
	want := strings.Join([]string{
		f + "-2-two", f + ":3:HIT", f + "-4-four", "--",
		f + "-7-seven", f + ":8:HIT", f + "-9-nine",
	}, "\n")
	if out != want {
		t.Errorf("context output:\n%s\nwant:\n%s", out, want)
	}
}

func TestGrepContextOverlapKeepsEachLineOnce(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.txt": "x\nHIT\nHIT\ny\n"})
	out, _ := runGrep(t, dir, map[string]any{"pattern": "HIT", "output_mode": "content", "-n": true, "-A": 2, "-B": 1})
	f := filepath.Join(dir, "a.txt")
	want := strings.Join([]string{f + "-1-x", f + ":2:HIT", f + ":3:HIT", f + "-4-y"}, "\n")
	if out != want {
		t.Errorf("overlapping context:\n%s\nwant:\n%s", out, want)
	}
}

func TestGrepTypeFilter(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.go": "needle\n", "b.py": "needle\n", "c.tsx": "needle\n"})
	out, _ := runGrep(t, dir, map[string]any{"pattern": "needle", "type": "ts"})
	if !strings.Contains(out, "c.tsx") || strings.Contains(out, "a.go") || strings.Contains(out, "b.py") {
		t.Errorf("type=ts should keep only the .tsx file:\n%s", out)
	}
	if _, err := runGrep(t, dir, map[string]any{"pattern": "needle", "type": "cobol"}); err == nil || !strings.Contains(err.Error(), "known types") {
		t.Errorf("unknown type should be rejected with the list, got %v", err)
	}
}

func TestGrepHeadLimit(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.txt": "hit 1\nhit 2\nhit 3\nhit 4\n"})
	out, _ := runGrep(t, dir, map[string]any{"pattern": "hit", "output_mode": "content", "head_limit": 2})
	lines := strings.Split(out, "\n")
	if len(lines) != 3 || !strings.Contains(lines[2], "showing 2 of 4") {
		t.Errorf("head_limit should show 2 lines and say how many there were:\n%s", out)
	}
}

func TestGrepMultilineShowsMatchAndLine(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"a.go": "package a\n\nfunc F() {\n\treturn\n}\n"})
	out, _ := runGrep(t, dir, map[string]any{"pattern": `func F\(\) \{.*?\}`, "multiline": true, "output_mode": "content", "-n": true})
	if !strings.Contains(out, ":3:func F() {") || !strings.Contains(out, "return") {
		t.Errorf("multiline content should show the match and its starting line:\n%s", out)
	}
}

func TestGrepAndGlobResolveRelativePath(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"sub/a.txt": "needle\n"})
	out, _ := runGrep(t, dir, map[string]any{"pattern": "needle", "path": "sub"})
	if !strings.Contains(out, filepath.Join(dir, "sub", "a.txt")) {
		t.Errorf("Grep path=sub should search the working dir's sub:\n%s", out)
	}

	g, err := NewGlob()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"pattern": "*.txt", "path": "sub"})
	res, err := g.Execute(context.Background(), Context{WorkingDir: dir}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res[0].Content, "a.txt") {
		t.Errorf("Glob path=sub should search the working dir's sub:\n%s", res[0].Content)
	}
}
