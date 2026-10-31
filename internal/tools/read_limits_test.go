package tools

import (
	"bytes"
	"context"
	"encoding/base64"
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
	} {
		res := readWith(t, r, dir, map[string]any{"file_path": write(name, data)})
		if !res.IsError || len(res.Images) != 0 {
			t.Errorf("%s: %+v, want a refusal and no image", name, res)
		}
	}
}

// An image over the size every request can carry is scaled down on read, not
// sent at full size. A screenshot read at full size was fine on its own and
// then, once the conversation held more than 20 images, made every request
// 400 (measured: 21 screenshots of 2634×2042 stranded a goshapp session). An
// image over the absolute limit is scaled too, rather than refused.
func TestReadScalesLargeImages(t *testing.T) {
	dir := t.TempDir()
	r, _ := NewRead()
	tests := []struct {
		name   string
		w, h   int
		ww, wh int
		note   bool
	}{
		{name: "the reported screenshot", w: 2634, h: 2042, ww: 2000, wh: 1550, note: true},
		{name: "over the absolute limit", w: 9000, h: 10, ww: 2000, wh: 2, note: true},
		{name: "already small enough", w: 1600, h: 1000, ww: 1600, wh: 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".png")
			if err := os.WriteFile(p, pngBytes(t, tc.w, tc.h), 0o644); err != nil {
				t.Fatal(err)
			}
			res := readWith(t, r, dir, map[string]any{"file_path": p})
			if res.IsError || len(res.Images) != 1 {
				t.Fatalf("read: %+v", res)
			}
			raw, err := base64.StdEncoding.DecodeString(res.Images[0].Base64)
			if err != nil {
				t.Fatal(err)
			}
			cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Width != tc.ww || cfg.Height != tc.wh {
				t.Errorf("sent %dx%d, want %dx%d", cfg.Width, cfg.Height, tc.ww, tc.wh)
			}
			if got := strings.Contains(res.Content, "scaled from"); got != tc.note {
				t.Errorf("content %q: scaled note present = %v, want %v", res.Content, got, tc.note)
			}
		})
	}
}
