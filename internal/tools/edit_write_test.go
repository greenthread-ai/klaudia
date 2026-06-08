package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/permission"
)

func TestWriteCreatesFileAndDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "nested", "out.txt")

	w, err := NewWrite()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(WriteInput{FilePath: path, Content: "hello\n"})
	res, err := w.Execute(context.Background(), Context{}, raw)
	if err != nil || res[0].IsError {
		t.Fatalf("write failed: err=%v res=%+v", err, res[0])
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello\n" {
		t.Errorf("file content = %q, want %q", got, "hello\n")
	}
}

func TestEditUniqueReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("foo bar baz\n"), 0o644)

	e, _ := NewEdit()
	raw, _ := json.Marshal(EditInput{FilePath: path, OldString: "bar", NewString: "QUX"})
	res, err := e.Execute(context.Background(), Context{}, raw)
	if err != nil || res[0].IsError {
		t.Fatalf("edit failed: err=%v res=%+v", err, res[0])
	}
	got, _ := os.ReadFile(path)
	if string(got) != "foo QUX baz\n" {
		t.Errorf("content = %q", got)
	}
}

func TestEditAmbiguousWithoutReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	os.WriteFile(path, []byte("x x x\n"), 0o644)

	e, _ := NewEdit()
	raw, _ := json.Marshal(EditInput{FilePath: path, OldString: "x", NewString: "y"})
	res, _ := e.Execute(context.Background(), Context{}, raw)
	if !res[0].IsError {
		t.Error("expected error for non-unique old_string")
	}
	// replace_all succeeds.
	raw, _ = json.Marshal(EditInput{FilePath: path, OldString: "x", NewString: "y", ReplaceAll: true})
	res, _ = e.Execute(context.Background(), Context{}, raw)
	if res[0].IsError {
		t.Errorf("replace_all should succeed: %+v", res[0])
	}
	got, _ := os.ReadFile(path)
	if string(got) != "y y y\n" {
		t.Errorf("content = %q", got)
	}
}

func TestEditNormalizesLineEndings(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		oldString string
		newString string
		want      string
	}{
		{
			name:      "crlf input for lf file",
			content:   "alpha\nbeta\ngamma\n",
			oldString: "alpha\r\nbeta\r\n",
			newString: "one\r\ntwo\r\n",
			want:      "one\ntwo\ngamma\n",
		},
		{
			name:      "lf input for crlf file",
			content:   "alpha\r\nbeta\r\ngamma\r\n",
			oldString: "alpha\nbeta\n",
			newString: "one\ntwo\n",
			want:      "one\r\ntwo\r\ngamma\r\n",
		},
	}

	e, _ := NewEdit()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f.txt")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(EditInput{FilePath: path, OldString: c.oldString, NewString: c.newString})
			res, err := e.Execute(context.Background(), Context{}, raw)
			if err != nil || res[0].IsError {
				t.Fatalf("edit failed: err=%v res=%+v", err, res[0])
			}
			got, _ := os.ReadFile(path)
			if string(got) != c.want {
				t.Errorf("content = %q, want %q", got, c.want)
			}
		})
	}
}

func TestEditNotFoundIncludesHint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	// "alpha" appears only mid-word, so neither an exact nor a line-aligned
	// (whitespace-flexible) match succeeds — but the trimmed text is present, so
	// the hint should point that out.
	if err := os.WriteFile(path, []byte("return alphabet\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e, _ := NewEdit()
	raw, _ := json.Marshal(EditInput{FilePath: path, OldString: "alpha ", NewString: "beta"})
	res, err := e.Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !res[0].IsError || !strings.Contains(res[0].Content, "trimmed text exists") {
		t.Fatalf("expected trimmed-text hint, got %+v", res[0])
	}
}

func TestEditValidateRejectsSameStrings(t *testing.T) {
	e, _ := NewEdit()
	raw, _ := json.Marshal(EditInput{FilePath: "/abs/f.txt", OldString: "a", NewString: "a"})
	if err := e.ValidateInput(raw); err == nil {
		t.Error("expected error when old_string == new_string")
	}
}

// editClassPermission verifies the mode-dependent intrinsic decisions through
// the central Check flow for a mutating tool.
func TestEditClassPermissionByMode(t *testing.T) {
	e, _ := NewEdit()
	raw, _ := json.Marshal(EditInput{FilePath: "/abs/f.txt", OldString: "a", NewString: "b"})
	req := e.PermissionRequest(raw)

	cases := []struct {
		mode permission.Mode
		want permission.Behavior
	}{
		{permission.ModeDefault, permission.Ask},
		{permission.ModeAcceptEdits, permission.Allow},
		{permission.ModePlan, permission.Deny},
		{permission.ModeDontAsk, permission.Deny},
		{permission.ModeBypassPermissions, permission.Allow},
	}
	for _, c := range cases {
		got := permission.Check(permission.Context{Mode: permission.StaticMode(c.mode)}, e, req)
		if got.Behavior != c.want {
			t.Errorf("mode %s: behavior = %q, want %q", c.mode, got.Behavior, c.want)
		}
	}
	if req.Specifier != "/abs/f.txt" {
		t.Errorf("specifier = %q, want the file path", req.Specifier)
	}
}

func TestEditFlexibleWhitespaceMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	// File is tab-indented.
	os.WriteFile(path, []byte("func main() {\n\tx := 1\n\tprintln(x)\n}\n"), 0o644)

	e, _ := NewEdit()
	// Model supplies the block space-indented with a stray trailing space, so
	// there is no exact match — only a whitespace-flexible one.
	raw, _ := json.Marshal(EditInput{
		FilePath:  path,
		OldString: "    x := 1  \n    println(x)",
		NewString: "\ty := 2\n\tprintln(y)",
	})
	res, err := e.Execute(context.Background(), Context{}, raw)
	if err != nil || res[0].IsError {
		t.Fatalf("flexible edit failed: err=%v res=%+v", err, res[0])
	}
	if want := "func main() {\n\ty := 2\n\tprintln(y)\n}\n"; func() bool {
		got, _ := os.ReadFile(path)
		return string(got) == want
	}() == false {
		got, _ := os.ReadFile(path)
		t.Errorf("content = %q", got)
	}
	if !strings.Contains(res[0].Content, "ignoring surrounding whitespace") {
		t.Errorf("expected flexible-match note, got %q", res[0].Content)
	}
}

func TestFlexibleMatch(t *testing.T) {
	const content = "func main() {\n\tx := 1\n\tprintln(x)\n}\n"

	t.Run("unique whitespace-only difference", func(t *testing.T) {
		span, ok := flexibleMatch(content, "    x := 1  \n  println(x)")
		if !ok || span != "\tx := 1\n\tprintln(x)" {
			t.Errorf("got (%q, %v)", span, ok)
		}
	})
	t.Run("no match", func(t *testing.T) {
		if span, ok := flexibleMatch(content, "y := 9"); ok {
			t.Errorf("expected no match, got %q", span)
		}
	})
	t.Run("ambiguous returns false", func(t *testing.T) {
		dup := "if x {\n    do()\n}\nif x {\n    do()\n}\n"
		if span, ok := flexibleMatch(dup, "if x {\ndo()\n}"); ok {
			t.Errorf("expected ambiguous match to be rejected, got %q", span)
		}
	})
	t.Run("blank old_string returns false", func(t *testing.T) {
		if _, ok := flexibleMatch(content, "   \n  "); ok {
			t.Error("blank old_string should not match")
		}
	})
}
