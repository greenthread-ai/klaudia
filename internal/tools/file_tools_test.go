package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// --- Edit ---

func TestEditValidateInput(t *testing.T) {
	e, _ := NewEdit()
	cases := []struct {
		raw     string
		wantErr string
	}{
		{`{"file_path":"f","old_string":"a","new_string":"b"}`, ""},
		{`{"file_path":" ","old_string":"a","new_string":"b"}`, "file_path is required"},
		{`{"file_path":"f","old_string":"a"}`, "new_string"},
	}
	for _, c := range cases {
		err := e.ValidateInput(json.RawMessage(c.raw))
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.raw, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: err = %v, want %q", c.raw, err, c.wantErr)
		}
	}
}

func TestEditResolvesRelativePathAndKeepsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.env")
	writeFile(t, path, "TOKEN=old\n", 0o600)

	e, _ := NewEdit()
	res := runTool(t, e, Context{WorkingDir: dir}, EditInput{FilePath: "secret.env", OldString: "old", NewString: "new"})
	if res.IsError || res.Content != "Edited "+path+" (1 replacement(s))" {
		t.Fatalf("res = %+v", res)
	}
	if got := readFile(t, path); got != "TOKEN=new\n" {
		t.Errorf("content = %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want the original 0600 kept", info.Mode().Perm())
	}
}

func TestEditReplaceAllCountsReplacements(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a-a-a", 0o644)
	e, _ := NewEdit()
	res := runTool(t, e, Context{}, EditInput{FilePath: path, OldString: "a", NewString: "b", ReplaceAll: true})
	if !strings.HasSuffix(res.Content, "(3 replacement(s))") {
		t.Errorf("content = %q, want the count of replacements", res.Content)
	}
}

func TestEditFailuresLeaveTheFileAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	body := "if x {\n\treturn\n}\nif x {\n    return\n}\n"
	writeFile(t, path, body, 0o644)

	cases := []struct {
		name string
		in   EditInput
		want string
	}{
		{"missing file", EditInput{FilePath: filepath.Join(dir, "nope.go"), OldString: "a", NewString: "b"}, "File does not exist: "},
		{"a directory", EditInput{FilePath: dir, OldString: "a", NewString: "b"}, "Error reading file:"},
		{"whitespace-only old_string", EditInput{FilePath: path, OldString: "   \n", NewString: "b"}, "old_string is empty or whitespace-only."},
		{"no near match", EditInput{FilePath: path, OldString: "for {}", NewString: "b"}, "Use Read immediately before Edit"},
		// The two blocks differ only in indentation, so a whitespace-tolerant
		// match finds both; ambiguity must be an error, never a guess.
		{"ambiguous flexible match", EditInput{FilePath: path, OldString: "if x {\n  return\n}", NewString: "b"}, "old_string not found"},
	}
	e, _ := NewEdit()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := runTool(t, e, Context{}, c.in)
			if !res.IsError || !strings.Contains(res.Content, c.want) {
				t.Errorf("res = %+v, want an error containing %q", res, c.want)
			}
			if got := readFile(t, path); got != body {
				t.Errorf("file changed on a failed edit: %q", got)
			}
		})
	}
}

func TestEditReportsAWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	path := filepath.Join(t.TempDir(), "ro.txt")
	writeFile(t, path, "abc", 0o444)
	e, _ := NewEdit()
	res := runTool(t, e, Context{}, EditInput{FilePath: path, OldString: "b", NewString: "B"})
	if !res.IsError || !strings.HasPrefix(res.Content, "Error writing file:") {
		t.Errorf("res = %+v", res)
	}
}

// --- Write ---

func TestWriteValidateInput(t *testing.T) {
	w := mustWrite(t)
	if err := w.ValidateInput(json.RawMessage(`{"file_path":"a","content":""}`)); err != nil {
		t.Errorf("empty content is a valid write: %v", err)
	}
	if err := w.ValidateInput(json.RawMessage(`{"file_path":"  ","content":"x"}`)); err == nil || !strings.Contains(err.Error(), "file_path is required") {
		t.Errorf("blank path: err = %v", err)
	}
}

func TestWriteResolvesRelativePathAndOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	writeFile(t, path, "old contents that are longer", 0o644)
	w := mustWrite(t)
	res := runTool(t, w, Context{WorkingDir: dir}, WriteInput{FilePath: "out.txt", Content: "new"})
	if res.IsError || res.Content != "File written successfully to "+path {
		t.Fatalf("res = %+v", res)
	}
	if got := readFile(t, path); got != "new" {
		t.Errorf("content = %q, want a full overwrite", got)
	}
}

func TestWriteFailures(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	writeFile(t, blocker, "", 0o644)
	w := mustWrite(t)

	res := runTool(t, w, Context{}, WriteInput{FilePath: filepath.Join(blocker, "child.txt"), Content: "x"})
	if !res.IsError || !strings.HasPrefix(res.Content, "Error creating parent directory:") {
		t.Errorf("parent is a file: %+v", res)
	}
	res = runTool(t, w, Context{}, WriteInput{FilePath: dir, Content: "x"})
	if !res.IsError || !strings.HasPrefix(res.Content, "Error writing file:") {
		t.Errorf("target is a directory: %+v", res)
	}
}

// --- Read ---

func TestReadTruncatesVeryLongLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.txt")
	writeFile(t, path, strings.Repeat("x", readMaxLineLen+500)+"\nshort\n", 0o644)
	res := runTool(t, mustRead(t), Context{}, ReadInput{FilePath: path})
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if got := len(strings.SplitN(lines[0], "\t", 2)[1]); got != readMaxLineLen {
		t.Errorf("first line length = %d, want it cut to %d", got, readMaxLineLen)
	}
	if lines[1] != "     2\tshort" {
		t.Errorf("second line = %q", lines[1])
	}
}

func TestReadEmptyAndPastTheEnd(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	writeFile(t, empty, "", 0o644)
	short := filepath.Join(dir, "short.txt")
	writeFile(t, short, "one\ntwo\n", 0o644)
	r := mustRead(t)
	for _, in := range []ReadInput{{FilePath: empty}, {FilePath: short, Offset: 10}} {
		res := runTool(t, r, Context{}, in)
		if res.IsError || res.Content != "<file is empty or offset is past end of file>" {
			t.Errorf("%+v: res = %+v", in, res)
		}
	}
}

func TestReadLineTooLongForTheScannerIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "huge.txt")
	writeFile(t, path, strings.Repeat("y", 2<<20), 0o644)
	res := runTool(t, mustRead(t), Context{}, ReadInput{FilePath: path})
	if !res.IsError || !strings.HasPrefix(res.Content, "Error reading file:") {
		t.Errorf("res = %+v", res)
	}
}

func TestReadImageAndPDFFailures(t *testing.T) {
	dir := t.TempDir()
	notPDF := filepath.Join(dir, "doc.PDF")
	writeFile(t, notPDF, "not a pdf", 0o644)
	r := mustRead(t)

	res := runTool(t, r, Context{}, ReadInput{FilePath: filepath.Join(dir, "missing.png")})
	if !res.IsError || !strings.HasPrefix(res.Content, "Error reading image:") {
		t.Errorf("missing image: %+v", res)
	}
	res = runTool(t, r, Context{}, ReadInput{FilePath: notPDF})
	if !res.IsError || !strings.HasPrefix(res.Content, "Error reading PDF:") {
		t.Errorf("bad PDF (upper-case extension still routes to the PDF reader): %+v", res)
	}
}

func TestReadImageMediaTypes(t *testing.T) {
	dir := t.TempDir()
	r := mustRead(t)
	for name, want := range map[string]string{
		"a.gif":  "image/gif",
		"a.webp": "image/webp",
		"a.JPEG": "image/jpeg",
	} {
		p := filepath.Join(dir, name)
		writeFile(t, p, "bytes", 0o644)
		res := runTool(t, r, Context{}, ReadInput{FilePath: p})
		if res.IsError || len(res.Images) != 1 || res.Images[0].MediaType != want {
			t.Errorf("%s: res = %+v, want one %s image", name, res, want)
		}
	}
	if res, _ := readImage("x.bmp"); !res[0].IsError || !strings.Contains(res[0].Content, "Unsupported image type: .bmp") {
		t.Errorf("unsupported type: %+v", res[0])
	}
}

// --- NotebookEdit ---

func TestNotebookValidateInput(t *testing.T) {
	ne, _ := NewNotebookEdit()
	cases := []struct {
		in      NotebookEditInput
		wantErr string
	}{
		{NotebookEditInput{NotebookPath: "n.ipynb", NewSource: "x"}, ""}, // replace is the default
		{NotebookEditInput{NotebookPath: "n.ipynb", EditMode: "delete", CellID: "c1"}, ""},
		{NotebookEditInput{NotebookPath: "n.ipynb", EditMode: "insert", CellType: "code"}, ""},
		{NotebookEditInput{NotebookPath: " ", NewSource: "x"}, "notebook_path is required"},
		{NotebookEditInput{NotebookPath: "n.ipynb", EditMode: "append"}, `edit_mode "append" is invalid`},
		{NotebookEditInput{NotebookPath: "n.ipynb", EditMode: "insert", CellType: "raw"}, "insert requires cell_type"},
	}
	for _, c := range cases {
		raw, _ := json.Marshal(c.in)
		err := ne.ValidateInput(raw)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%+v: unexpected error %v", c.in, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%+v: err = %v, want %q", c.in, err, c.wantErr)
		}
	}
}

func TestNotebookReplaceByIndexCanChangeCellType(t *testing.T) {
	path := writeNotebook(t)
	ne, _ := NewNotebookEdit()
	res := runTool(t, ne, Context{WorkingDir: filepath.Dir(path)}, NotebookEditInput{
		NotebookPath: filepath.Base(path), CellID: "1", NewSource: "x = 1\ny = 2", CellType: "code",
	})
	if res.IsError || res.Content != "Notebook "+path+" updated (replace)" {
		t.Fatalf("res = %+v", res)
	}
	cell := loadCells(t, path)[1].(map[string]any)
	if cell["cell_type"] != "code" {
		t.Errorf("cell_type = %v, want code", cell["cell_type"])
	}
	if got := cell["source"].([]any); !reflect.DeepEqual(got, []any{"x = 1\n", "y = 2"}) {
		t.Errorf("source = %#v, want ipynb line list", got)
	}
}

func TestNotebookInsertAtTopAddsACodeCellShape(t *testing.T) {
	path := writeNotebook(t)
	ne, _ := NewNotebookEdit()
	res := runTool(t, ne, Context{}, NotebookEditInput{NotebookPath: path, EditMode: "insert", CellType: "code", NewSource: "import os\n"})
	if res.IsError {
		t.Fatalf("res = %+v", res)
	}
	cells := loadCells(t, path)
	if len(cells) != 3 {
		t.Fatalf("cells = %d, want 3", len(cells))
	}
	top := cells[0].(map[string]any)
	if top["cell_type"] != "code" || top["execution_count"] != nil {
		t.Errorf("top cell = %v", top)
	}
	if outs, ok := top["outputs"].([]any); !ok || len(outs) != 0 {
		t.Errorf("a new code cell needs an empty outputs list, got %v", top["outputs"])
	}
	if id := cells[1].(map[string]any)["id"]; id != "c1" {
		t.Errorf("the old first cell moved to %v, want it second", id)
	}
}

func TestNotebookErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.ipynb")
	writeFile(t, bad, "{not json", 0o644)
	good := writeNotebook(t)
	before := readFile(t, good)

	cases := []struct {
		name string
		in   NotebookEditInput
		want string
	}{
		{"missing file", NotebookEditInput{NotebookPath: filepath.Join(dir, "none.ipynb")}, "Error reading notebook:"},
		{"invalid json", NotebookEditInput{NotebookPath: bad}, "Notebook is not valid JSON:"},
		{"replace unknown cell", NotebookEditInput{NotebookPath: good, CellID: "zz", NewSource: "x"}, `Cell "zz" not found`},
		{"replace with no id", NotebookEditInput{NotebookPath: good, NewSource: "x"}, `Cell "" not found`},
		{"delete out of range", NotebookEditInput{NotebookPath: good, CellID: "7", EditMode: "delete"}, `Cell "7" not found`},
		{"negative index", NotebookEditInput{NotebookPath: good, CellID: "-1", EditMode: "delete"}, `Cell "-1" not found`},
	}
	ne, _ := NewNotebookEdit()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := runTool(t, ne, Context{}, c.in)
			if !res.IsError || !strings.HasPrefix(res.Content, c.want) {
				t.Errorf("res = %+v, want %q", res, c.want)
			}
		})
	}
	if readFile(t, good) != before {
		t.Error("a failed edit rewrote the notebook")
	}
}

func TestNotebookHelpers(t *testing.T) {
	if got := sourceLines(""); len(got) != 0 {
		t.Errorf("sourceLines(\"\") = %v", got)
	}
	if got := sourceLines("a\nb\n"); !reflect.DeepEqual(got, []any{"a\n", "b\n"}) {
		t.Errorf("sourceLines keeps trailing newlines: %#v", got)
	}
	s := []any{"a", "b"}
	if got := insertAt(append([]any(nil), s...), -3, "x"); !reflect.DeepEqual(got, []any{"x", "a", "b"}) {
		t.Errorf("insertAt below range = %v", got)
	}
	if got := insertAt(append([]any(nil), s...), 99, "x"); !reflect.DeepEqual(got, []any{"a", "b", "x"}) {
		t.Errorf("insertAt past end = %v", got)
	}
}
