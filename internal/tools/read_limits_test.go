package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func readWith(t *testing.T, r *Read, dir string, input map[string]any) Result {
	t.Helper()
	raw, _ := json.Marshal(input)
	res, err := r.Execute(context.Background(), Context{WorkingDir: dir}, raw)
	if err != nil {
		t.Fatal(err)
	}
	return res[0]
}

// One line longer than the old 1 MB scanner limit failed the whole read.
func TestReadLongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "min.js")
	os.WriteFile(path, []byte("first\n"+strings.Repeat("é", 1<<20)+"\nlast"), 0o644)
	r, _ := NewRead()
	res := readWith(t, r, dir, map[string]any{"file_path": path})
	if res.IsError {
		t.Fatalf("read failed: %s", res.Content)
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "first") || !strings.HasSuffix(lines[2], "last") {
		t.Fatalf("got %d lines: %q…", len(lines), res.Content[:80])
	}
	if !utf8.ValidString(lines[1]) || !strings.Contains(lines[1], "line truncated") {
		t.Errorf("long line not cut cleanly: valid=%v, %q", utf8.ValidString(lines[1]), lines[1][len(lines[1])-60:])
	}
	if len(lines[1]) > readMaxLineLen+100 {
		t.Errorf("long line kept %d bytes", len(lines[1]))
	}
}

func TestReadCRLFAndNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "w.txt")
	os.WriteFile(path, []byte("a\r\nb"), 0o644)
	r, _ := NewRead()
	got := readWith(t, r, dir, map[string]any{"file_path": path}).Content
	if got != "     1\ta\n     2\tb\n" {
		t.Errorf("got %q", got)
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// Images were typed by extension and sent unchecked: an empty or mislabelled
// file, or one over the API's limits, was rejected — and, staying in the
// conversation, rejected again on every later request.
func TestReadImageChecks(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRead()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, data, 0o644)
		return p
	}

	ok := readWith(t, r, dir, map[string]any{"file_path": write("ok.png", pngBytes(t, 10, 10))})
	if ok.IsError || len(ok.Images) != 1 || ok.Images[0].MediaType != "image/png" {
		t.Errorf("valid png: %+v", ok)
	}
	// A PNG named .jpg is sent as what it is.
	named := readWith(t, r, dir, map[string]any{"file_path": write("wrong.jpg", pngBytes(t, 10, 10))})
	if named.IsError || named.Images[0].MediaType != "image/png" {
		t.Errorf("mislabelled png: %+v", named)
	}
	for name, data := range map[string][]byte{
		"empty.png": {},
		"page.png":  []byte("<!doctype html><html>404</html>"),
		"huge.png":  pngBytes(t, 9000, 10),
	} {
		res := readWith(t, r, dir, map[string]any{"file_path": write(name, data)})
		if !res.IsError || len(res.Images) != 0 {
			t.Errorf("%s: %+v, want a refusal and no image", name, res)
		}
	}
}
