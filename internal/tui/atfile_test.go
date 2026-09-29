package tui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tiny 1x1 PNG, the same fixture the Read-image test uses.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+M8AAAMBAQDJ/pLvAAAAAElFTkSuQmCC"

func writeTemp(t *testing.T, dir, name, contents string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExpandAtFilesInlinesContents(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "notes.txt", "hello\nworld\n")

	res := expandAtFiles("please read @notes.txt carefully", dir)

	if len(res.Notes) != 0 {
		t.Fatalf("unexpected notes: %v", res.Notes)
	}
	if len(res.Images) != 0 {
		t.Fatalf("unexpected images: %d", len(res.Images))
	}
	if !strings.Contains(res.Prompt, "please read @notes.txt carefully") {
		t.Errorf("original text was dropped:\n%s", res.Prompt)
	}
	if !strings.Contains(res.Prompt, "===== notes.txt =====") {
		t.Errorf("missing delimiter:\n%s", res.Prompt)
	}
	if !strings.Contains(res.Prompt, "hello\nworld") {
		t.Errorf("file contents not inlined:\n%s", res.Prompt)
	}
}

func TestExpandAtFilesLineRange(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "code.go", "one\ntwo\nthree\nfour\nfive\n")

	res := expandAtFiles("look at @code.go:2-4", dir)
	if len(res.Notes) != 0 {
		t.Fatalf("unexpected notes: %v", res.Notes)
	}
	if !strings.Contains(res.Prompt, "===== code.go:2-4 =====") {
		t.Errorf("range label missing:\n%s", res.Prompt)
	}
	body := res.Prompt[strings.Index(res.Prompt, "====="):]
	if !strings.Contains(body, "two\nthree\nfour") {
		t.Errorf("wrong lines selected:\n%s", body)
	}
	if strings.Contains(body, "one") || strings.Contains(body, "five") {
		t.Errorf("range leaked lines outside 2-4:\n%s", body)
	}
}

func TestExpandAtFilesSingleLine(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "code.go", "one\ntwo\nthree\n")

	res := expandAtFiles("@code.go:2", dir)
	body := res.Prompt[strings.Index(res.Prompt, "====="):]
	if !strings.Contains(body, "two") || strings.Contains(body, "one") || strings.Contains(body, "three") {
		t.Errorf("single-line select wrong:\n%s", body)
	}
	if !strings.Contains(res.Prompt, "===== code.go:2-2 =====") {
		t.Errorf("single-line label:\n%s", res.Prompt)
	}
}

func TestExpandAtFilesRangeOutsideFile(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "short.txt", "a\nb\n")

	res := expandAtFiles("@short.txt:9-10", dir)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "outside the file") {
		t.Fatalf("expected out-of-range note, got %v", res.Notes)
	}
	if strings.Contains(res.Prompt, "=====") {
		t.Errorf("should not inline anything:\n%s", res.Prompt)
	}
}

func TestExpandAtFilesEndClampedToFile(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "short.txt", "a\nb\nc\n")

	// End past EOF is clamped rather than refused, so "@f:1-9999" is the whole file.
	res := expandAtFiles("@short.txt:1-9999", dir)
	if len(res.Notes) != 0 {
		t.Fatalf("unexpected notes: %v", res.Notes)
	}
	body := res.Prompt[strings.Index(res.Prompt, "====="):]
	if !strings.Contains(body, "a\nb\nc") {
		t.Errorf("clamped range missing lines:\n%s", body)
	}
}

func TestExpandAtFilesImageAttachment(t *testing.T) {
	dir := t.TempDir()
	raw, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	res := expandAtFiles("what is in @shot.png", dir)
	if len(res.Notes) != 0 {
		t.Fatalf("unexpected notes: %v", res.Notes)
	}
	if len(res.Images) != 1 {
		t.Fatalf("expected 1 image attachment, got %d", len(res.Images))
	}
	if res.Images[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", res.Images[0].MediaType)
	}
	if res.Images[0].Base64 != onePixelPNG {
		t.Errorf("image bytes not preserved as base64")
	}
	// The image is NOT dumped into the text; the @token stays as the marker.
	if strings.Contains(res.Prompt, "=====") {
		t.Errorf("image should not be inlined as text:\n%s", res.Prompt)
	}
	if !strings.Contains(res.Prompt, "@shot.png") {
		t.Errorf("image reference should remain in prompt:\n%s", res.Prompt)
	}
}

func TestExpandAtFilesMissingFile(t *testing.T) {
	dir := t.TempDir()

	res := expandAtFiles("read @does/not/exist.go now", dir)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "no such file") {
		t.Fatalf("expected a clear missing-file note, got %v", res.Notes)
	}
	if strings.Contains(res.Prompt, "=====") {
		t.Errorf("nothing should be inlined:\n%s", res.Prompt)
	}
}

func TestExpandAtFilesBareHandleIgnored(t *testing.T) {
	dir := t.TempDir()

	// "@someone" is not a path (no slash, no extension) and does not exist, so it
	// is left untouched with no note — an email/mention must survive.
	res := expandAtFiles("cc @someone on this", dir)
	if len(res.Notes) != 0 {
		t.Fatalf("bare handle should not warn: %v", res.Notes)
	}
	if res.Prompt != "cc @someone on this" {
		t.Errorf("prompt was rewritten:\n%s", res.Prompt)
	}
}

func TestExpandAtFilesOversizedTextRefused(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", atFileMaxBytes+1)
	writeTemp(t, dir, "big.log", big)

	res := expandAtFiles("@big.log", dir)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "over the") {
		t.Fatalf("expected oversized note, got %v", res.Notes)
	}
	if strings.Contains(res.Prompt, "xxxx") {
		t.Errorf("oversized file was inlined anyway")
	}
}

func TestExpandAtFilesBinaryRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.out"), []byte{0x7f, 'E', 'L', 'F', 0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}

	res := expandAtFiles("@a.out", dir)
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "binary") {
		t.Fatalf("expected binary note, got %v", res.Notes)
	}
	if strings.Contains(res.Prompt, "=====") {
		t.Errorf("binary bytes should not be inlined:\n%s", res.Prompt)
	}
}

func TestExpandAtFilesNoRefsUnchanged(t *testing.T) {
	res := expandAtFiles("just some prose with no references", "/tmp")
	if res.Prompt != "just some prose with no references" {
		t.Errorf("prompt changed:\n%s", res.Prompt)
	}
	if len(res.Images) != 0 || len(res.Notes) != 0 {
		t.Errorf("unexpected images/notes: %v %v", res.Images, res.Notes)
	}
}

func TestExpandAtFilesDedupesRepeatedRef(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "x.txt", "content")

	res := expandAtFiles("@x.txt and again @x.txt", dir)
	if n := strings.Count(res.Prompt, "===== x.txt ====="); n != 1 {
		t.Errorf("expected one section for a repeated ref, got %d", n)
	}
}

func TestExpandAtFilesTokenPunctuation(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "readme.md", "docs")

	// Trailing sentence punctuation must not defeat the reference.
	res := expandAtFiles("see @readme.md.", dir)
	if !strings.Contains(res.Prompt, "===== readme.md =====") {
		t.Errorf("trailing period defeated the reference:\n%s", res.Prompt)
	}
}

func TestScanAtTokensMidWordIgnored(t *testing.T) {
	// An @ inside a word (an email local@host) does not open a token.
	toks := scanAtTokens("mail me at user@example.com please")
	for _, tk := range toks {
		if strings.Contains(tk, "example.com") && !strings.HasPrefix(tk, "example") {
			t.Errorf("mid-word @ was treated as a token: %q", tk)
		}
	}
}

func TestParseRange(t *testing.T) {
	cases := []struct {
		in         string
		start, end int
		ok         bool
	}{
		{"5", 5, 5, true},
		{"2-4", 2, 4, true},
		{"0", 0, 0, false},
		{"4-2", 0, 0, false},
		{"x", 0, 0, false},
		{"1-", 0, 0, false},
		{"443", 443, 443, true},
	}
	for _, c := range cases {
		s, e, ok := parseRange(c.in)
		if ok != c.ok || s != c.start || e != c.end {
			t.Errorf("parseRange(%q) = (%d,%d,%v), want (%d,%d,%v)", c.in, s, e, ok, c.start, c.end, c.ok)
		}
	}
}
