package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readContent(t *testing.T, in ReadInput) string {
	t.Helper()
	raw, _ := json.Marshal(in)
	res, err := mustRead(t).Execute(context.Background(), Context{}, raw)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res[0].IsError {
		t.Fatalf("unexpected error result: %s", res[0].Content)
	}
	return res[0].Content
}

// Hitting the line limit used to end the result with no sign that the file
// went on, so the model could take a window for the whole file.
func TestReadNoticeWhenLimitCutsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.txt")
	var sb strings.Builder
	for i := 1; i <= readDefaultLimit+500; i++ {
		fmt.Fprintf(&sb, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readContent(t, ReadInput{FilePath: path})
	want := fmt.Sprintf("\n(showing lines 1–2000 of %d; pass offset=2001 to continue)\n", readDefaultLimit+500)
	if !strings.HasSuffix(got, want) {
		t.Errorf("default read ends %q, want suffix %q", got[len(got)-80:], want)
	}

	got = readContent(t, ReadInput{FilePath: path, Offset: 2001, Limit: 100})
	if !strings.HasSuffix(got, "(showing lines 2001–2100 of 2500; pass offset=2101 to continue)\n") {
		t.Errorf("windowed read ends %q", got[len(got)-80:])
	}
}

// No notice when the window reaches the end of the file, including a final
// line without a newline and a limit that lands exactly on the last line.
func TestReadNoNoticeAtEndOfFile(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"newline.txt":   "a\nb\nc\n",
		"nonewline.txt": "a\nb\nc",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, in := range []ReadInput{
			{FilePath: path},
			{FilePath: path, Limit: 3},
			{FilePath: path, Offset: 2, Limit: 2},
		} {
			if got := readContent(t, in); strings.Contains(got, "showing lines") {
				t.Errorf("%s %+v: unexpected notice in %q", name, in, got)
			}
		}
	}

	// A final line without a newline still counts toward the total.
	path := filepath.Join(dir, "nonewline.txt")
	got := readContent(t, ReadInput{FilePath: path, Limit: 2})
	if !strings.HasSuffix(got, "(showing lines 1–2 of 3; pass offset=3 to continue)\n") {
		t.Errorf("got %q", got)
	}
}

// A binary file was dumped as lines of noise; it is now reported by size.
func TestReadBinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.bin")
	data := append([]byte("\x7fELF\x02\x01\x01"), make([]byte, 100)...)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got := readContent(t, ReadInput{FilePath: path})
	if want := fmt.Sprintf("<binary file, %d bytes; not shown as text>", len(data)); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A NUL past the sniffed window does not make a text file binary, and
	// the sniff leaves the file rewound for the line reader.
	path = filepath.Join(dir, "late-nul.txt")
	text := strings.Repeat("x", readSniffLen) + "\n\x00\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	got = readContent(t, ReadInput{FilePath: path, Limit: 1})
	if !strings.HasPrefix(got, "     1\txxx") || strings.Contains(got, "binary") {
		t.Errorf("text file with a late NUL: %q", got[:40])
	}
}
