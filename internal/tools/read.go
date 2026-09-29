package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/greenthread-ai/klaudia/internal/native/pdf"
	"github.com/greenthread-ai/klaudia/internal/permission"
	"github.com/greenthread-ai/klaudia/internal/schema"
)

// readPDF extracts text from a PDF and returns it with a page-count header.
func readPDF(path string) ([]Result, error) {
	text, err := pdf.ExtractText(path)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading PDF: %v", err), IsError: true}}, nil
	}
	pages, _ := pdf.PageCount(path)
	if strings.TrimSpace(text) == "" {
		return []Result{{Content: fmt.Sprintf("<PDF with %d page(s) and no extractable text (it may be scanned/image-only)>", pages)}}, nil
	}
	return []Result{{Content: fmt.Sprintf("[PDF: %d page(s)]\n\n%s", pages, text)}}, nil
}

func readImage(path string) ([]Result, error) {
	ext := strings.ToLower(filepath.Ext(path))
	mediaType := ""
	switch ext {
	case ".png":
		mediaType = "image/png"
	case ".jpg", ".jpeg":
		mediaType = "image/jpeg"
	case ".gif":
		mediaType = "image/gif"
	case ".webp":
		mediaType = "image/webp"
	default:
		return []Result{{Content: fmt.Sprintf("Unsupported image type: %s", ext), IsError: true}}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading image: %v", err), IsError: true}}, nil
	}

	return []Result{{
		Content: fmt.Sprintf("[image: %s]", path),
		Images:  []ResultImage{{MediaType: mediaType, Base64: base64.StdEncoding.EncodeToString(data)}},
	}}, nil
}

func isImageExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}

// readDefaultLimit is the default number of lines Read returns when no limit is
// given (the JS Read tool reads up to 2000 lines).
const readDefaultLimit = 2000

// readMaxLineLen caps individual line length; longer lines are truncated so a
// single huge line can't blow the context window.
const readMaxLineLen = 2000

// ReadInput is the Read tool's input. Tags drive both API schema generation and
// runtime validation (see internal/schema).
type ReadInput struct {
	FilePath string `json:"file_path" jsonschema:"description=The path to the file to read (absolute, or relative to the working directory)"`
	Offset   int    `json:"offset,omitempty" jsonschema:"description=The line number to start reading from (1-indexed)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=The number of lines to read"`
}

// Read reads a file from the local filesystem and returns it in cat -n format
// (line numbers starting at 1), mirroring the JS Read tool.
type Read struct {
	schema *schema.Schema
}

// NewRead constructs the Read tool, generating its input schema once.
func NewRead() (*Read, error) {
	s, err := schema.For[ReadInput]()
	if err != nil {
		return nil, fmt.Errorf("read: build schema: %w", err)
	}
	return &Read{schema: s}, nil
}

func (r *Read) Name() string { return "Read" }

func (r *Read) Description(context.Context) (string, error) {
	return "Reads a file from the local filesystem. file_path may be absolute or relative " +
		"to the working directory. " +
		"Text files return up to 2000 lines in cat -n format (line numbers from 1); use " +
		"offset and limit to window a large file; when lines remain past the window, the " +
		"result ends with a note giving the offset to continue from. Binary files are " +
		"reported by size, not shown. PDF files are returned as extracted text. " +
		"Image files (png, jpg, jpeg, gif, webp) are returned as viewable image blocks — use " +
		"Read to look at an image; do not assume you cannot see it.", nil
}

func (r *Read) InputSchema() json.RawMessage { return r.schema.Raw }

func (r *Read) ValidateInput(raw json.RawMessage) error {
	if err := r.schema.Validate(raw); err != nil {
		return err
	}
	var in ReadInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return err
	}
	if strings.TrimSpace(in.FilePath) == "" {
		return fmt.Errorf("file_path is required")
	}
	return nil
}

// PermissionRequest: Read needs no specifier (read-only, always allowed).
func (r *Read) PermissionRequest(json.RawMessage) permission.PermissionRequest {
	return permission.PermissionRequest{}
}

// CheckPermissions: Read is read-only and always allowed.
func (r *Read) CheckPermissions(pctx permission.Context, _ permission.PermissionRequest) permission.Decision {
	return allowAlways(pctx)
}

func (r *Read) Execute(_ context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in ReadInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	in.FilePath = resolvePath(tctx, in.FilePath) // accept paths relative to the working dir

	if info, statErr := os.Stat(in.FilePath); statErr == nil && info.IsDir() {
		return []Result{{Content: fmt.Sprintf("Path is a directory, not a file: %s. Use Glob or `ls` via Bash to list its contents.", in.FilePath), IsError: true}}, nil
	}

	// PDFs are read as extracted text (in-process via internal/native/pdf).
	if strings.EqualFold(filepath.Ext(in.FilePath), ".pdf") {
		return readPDF(in.FilePath)
	}

	if isImageExt(in.FilePath) {
		return readImage(in.FilePath)
	}

	f, err := os.Open(in.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Result{{Content: fmt.Sprintf("File does not exist: %s", in.FilePath), IsError: true}}, nil
		}
		return []Result{{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}}, nil
	}
	defer f.Close()

	// A binary file read as lines is noise that costs context and tells the
	// model nothing; say what it is instead.
	if size, binary := sniffBinary(f); binary {
		return []Result{{Content: fmt.Sprintf("<binary file, %d bytes; not shown as text>", size)}}, nil
	}

	start := max(in.Offset, 1)
	limit := in.Limit
	if limit <= 0 {
		limit = readDefaultLimit
	}

	var b strings.Builder
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	emitted := 0
	for sc.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if emitted >= limit {
			break
		}
		line := sc.Text()
		if len(line) > readMaxLineLen {
			line = line[:readMaxLineLen]
		}
		// cat -n format: line number right-aligned in a 6-wide field, then a tab.
		fmt.Fprintf(&b, "%6d\t%s\n", lineNo, line)
		emitted++
	}
	if err := sc.Err(); err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}}, nil
	}

	if emitted == 0 {
		return []Result{{Content: "<file is empty or offset is past end of file>"}}, nil
	}
	// Stopping at the limit silently let the model take the first 2,000 lines
	// for the whole file. Counting is a second pass, paid only when the limit
	// was reached.
	if emitted == limit {
		last := start + emitted - 1
		if total, err := countLines(f); err == nil && total > last {
			fmt.Fprintf(&b, "\n(showing lines %d–%d of %d; pass offset=%d to continue)\n", start, last, total, last+1)
		}
	}
	return []Result{{Content: b.String()}}, nil
}

// readSniffLen is how much of a file sniffBinary looks at: the same 8 KB
// window, and NUL-byte test, that Grep uses to skip binary files.
const readSniffLen = 8192

// sniffBinary reports the file's size and whether its first readSniffLen bytes
// hold a NUL, then rewinds f. A file that cannot be sniffed is treated as text,
// so the line reader reports any error itself.
func sniffBinary(f *os.File) (int64, bool) {
	buf := make([]byte, readSniffLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, false
	}
	if bytes.IndexByte(buf[:n], 0) < 0 {
		return 0, false
	}
	var size int64
	if info, err := f.Stat(); err == nil {
		size = info.Size()
	}
	return size, true
}

// countLines counts f's lines from its start the way the line reader splits
// them: one per newline, plus a final line that has none.
func countLines(f *os.File) (int, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	buf := make([]byte, 64*1024)
	lines := 0
	last := byte('\n')
	for {
		n, err := f.Read(buf)
		if n > 0 {
			lines += bytes.Count(buf[:n], []byte{'\n'})
			last = buf[n-1]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	if last != '\n' {
		lines++
	}
	return lines, nil
}
