package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPrefersTheFrontendsText(t *testing.T) {
	// Under ACP the frontend is the editor, which answers from the buffer it
	// has open. Reading disk while the user looks at unsaved edits is how the
	// model ends up reasoning about text that is no longer there — and how an
	// Edit gets built on an old_string the user has already replaced.
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("on disk\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path})
	res, err := r.Execute(context.Background(), Context{
		ReadText: func(context.Context, string, int, int) (string, error) {
			return "unsaved\n", nil
		},
	}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "     1\tunsaved\n"; res[0].Content != want {
		t.Errorf("content = %q, want %q", res[0].Content, want)
	}
}

func TestReadAsksTheFrontendForTheWindowItWants(t *testing.T) {
	// The hook is given the resolved path and the window, and what comes back
	// is already *cut* to it. Numbering still starts at the requested line, so
	// "     120" means line 120 whichever source served it.
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var gotPath string
	var gotLine, gotLimit int
	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path, Offset: 120, Limit: 2})
	res, err := r.Execute(context.Background(), Context{
		ReadText: func(_ context.Context, p string, line, limit int) (string, error) {
			gotPath, gotLine, gotLimit = p, line, limit
			return "one twenty\none twentyone\n", nil
		},
	}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPath != path || gotLine != 120 || gotLimit != 2 {
		t.Errorf("hook got (%q, %d, %d), want (%q, 120, 2)", gotPath, gotLine, gotLimit, path)
	}
	// The window came back full, so it says the file may go on; the editor's
	// buffer is not counted, so no total is given.
	want := "   120\tone twenty\n   121\tone twentyone\n\n(showing lines 120–121; there may be more: pass offset=122 to continue)\n"
	if res[0].Content != want {
		t.Errorf("content =\n%q\nwant\n%q", res[0].Content, want)
	}
}

func TestReadResolvesThePathBeforeAskingTheFrontend(t *testing.T) {
	// The ACP hook refuses anything that is not absolute, so a relative
	// file_path has to have been resolved by the time it gets there or every
	// relative Read silently loses the editor's buffer.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var got string
	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: "f.txt"})
	if _, err := r.Execute(context.Background(), Context{
		WorkingDir: dir,
		ReadText: func(_ context.Context, p string, _, _ int) (string, error) {
			got = p
			return "x\n", nil
		},
	}, raw); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("the hook was given %q, want an absolute path", got)
	}
}

func TestReadFallsBackToDiskWhenTheFrontendCannotServeIt(t *testing.T) {
	// The hook is an improvement on reading the file, never a restriction on
	// it: a client that cannot serve this path — not in its workspace, or the
	// request unanswered because the turn was cancelled — must not turn a
	// perfectly readable file into a failed Read.
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path, Offset: 2, Limit: 2})
	res, err := r.Execute(context.Background(), Context{
		ReadText: func(context.Context, string, int, int) (string, error) {
			return "", errors.New("outside the workspace")
		},
	}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "     2\tb\n     3\tc\n"
	if res[0].Content != want {
		t.Errorf("content = %q, want the file from disk %q", res[0].Content, want)
	}
}

func TestReadDoesNotAskTheFrontendForABinaryFile(t *testing.T) {
	// Images and PDFs are answered before the hook, because fs/read_text_file
	// returns text and a base64 image block is not something an editor buffer
	// can supply.
	dir := t.TempDir()
	path := filepath.Join(dir, "x.png")
	if err := os.WriteFile(path, pngBytes(t, 1, 1), 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path})
	res, err := r.Execute(context.Background(), Context{
		ReadText: func(context.Context, string, int, int) (string, error) {
			t.Error("an image was requested through fs/read_text_file")
			return "", nil
		},
	}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res[0].Images) != 1 {
		t.Errorf("got %+v, want an image block", res[0])
	}
}

func TestReadOfAFrontendServedEmptyWindow(t *testing.T) {
	// An offset past the end of the user's buffer is a legitimate answer of
	// "nothing", and the same message the disk path gives is the right one.
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := mustRead(t)
	raw, _ := json.Marshal(ReadInput{FilePath: path, Offset: 900})
	res, err := r.Execute(context.Background(), Context{
		ReadText: func(context.Context, string, int, int) (string, error) { return "", nil },
	}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(res[0].Content, "empty or offset is past end") {
		t.Errorf("content = %q", res[0].Content)
	}
}
