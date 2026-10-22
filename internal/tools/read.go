package tools

import (
	"bufio"
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

// ConcurrencySafe: opening a file for reading needs nothing a sibling call
// also needs, and Read never prompts. A batch of them is the commonest shape
// in a turn — the model orients itself by reading several files at once.
func (r *Read) ConcurrencySafe() bool { return true }

func (r *Read) Description(context.Context) (string, error) {
	return "Reads a file from the local filesystem. file_path may be absolute or relative " +
		"to the working directory. " +
		"Text files return up to 2000 lines in cat -n format (line numbers from 1); use " +
		"offset and limit to window a large file. PDF files are returned as extracted text. " +
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

func (r *Read) Execute(ctx context.Context, tctx Context, raw json.RawMessage) ([]Result, error) {
	var in ReadInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	in.FilePath = resolveExistingPath(tctx, in.FilePath) // relative to the working dir; see pathrepair.go

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

	start := max(in.Offset, 1)
	limit := in.Limit
	if limit <= 0 {
		limit = readDefaultLimit
	}

	// Ask the frontend first when it offers to serve the file. Under ACP that is
	// the editor, which returns the buffer *including unsaved edits* — the whole
	// reason the hook exists. It applies start/limit itself, so what comes back
	// is already the window and nothing is skipped; numbering still begins at
	// start so the line numbers mean the same thing either way.
	if tctx.ReadText != nil {
		text, rerr := tctx.ReadText(ctx, in.FilePath, start, limit)
		if rerr == nil {
			return numbered(strings.NewReader(text), 0, start, limit)
		}
		// Fall through to disk. The hook is an improvement on reading the file,
		// never a restriction on it: a client that cannot serve this path (not
		// in the project, not a text file, request unanswered because the turn
		// was cancelled) must not turn a readable file into a failed Read.
	}

	f, err := os.Open(in.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []Result{{Content: fmt.Sprintf("File does not exist: %s", in.FilePath), IsError: true}}, nil
		}
		return []Result{{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}}, nil
	}
	defer f.Close()

	return numbered(f, start-1, start, limit)
}

// numbered renders src in cat -n form: drop the first skip lines, emit at most
// limit of the rest, and number the first emitted line `first`.
//
// skip and first are separate because the two sources differ in exactly that
// way — a file on disk starts at line 1 and has to be wound forward, while a
// window the frontend already cut starts at the line the caller asked for.
func numbered(src io.Reader, skip, first, limit int) ([]Result, error) {
	var b strings.Builder
	sc := bufio.NewScanner(src)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	read := 0
	emitted := 0
	for sc.Scan() {
		read++
		if read <= skip {
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
		fmt.Fprintf(&b, "%6d\t%s\n", first+emitted, line)
		emitted++
	}
	if err := sc.Err(); err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}}, nil
	}

	if emitted == 0 {
		return []Result{{Content: "<file is empty or offset is past end of file>"}}, nil
	}
	return []Result{{Content: b.String()}}, nil
}
