package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

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

// Limits the API applies to an image block. An image over them is rejected,
// and because it stays in the conversation it is rejected again with every
// later request, so it is refused here instead.
const (
	maxImageBytes = 5 << 20
	maxImageSide  = 8000
)

func readImage(path string) ([]Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return []Result{{Content: fmt.Sprintf("Error reading image: %v", err), IsError: true}}, nil
	}
	if len(data) == 0 {
		return []Result{{Content: fmt.Sprintf("%s is empty (0 bytes); there is no image to show.", path), IsError: true}}, nil
	}
	// The type comes from the bytes, not the name. A ".png" that is really a
	// JPEG was sent labelled image/png and rejected; one that is not an image
	// at all (an HTML error page saved with the wrong name) was sent anyway.
	mediaType := http.DetectContentType(data)
	switch mediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return []Result{{Content: fmt.Sprintf("%s is named like an image but its content is %s, which cannot be shown as one.", path, mediaType), IsError: true}}, nil
	}
	if len(data) > maxImageBytes {
		return []Result{{Content: fmt.Sprintf("%s is %d bytes, over the %d-byte limit for an image; resize or compress it first (e.g. with ImageMagick: convert in.png -resize 50%% out.png).", path, len(data), maxImageBytes), IsError: true}}, nil
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && (cfg.Width > maxImageSide || cfg.Height > maxImageSide) {
		return []Result{{Content: fmt.Sprintf("%s is %dx%d pixels, over the %d-pixel limit on a side; resize it first.", path, cfg.Width, cfg.Height, maxImageSide), IsError: true}}, nil
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

	start := max(in.Offset, 1)
	limit := in.Limit
	if limit <= 0 {
		limit = readDefaultLimit
	}

	var b strings.Builder
	br := bufio.NewReaderSize(f, 64*1024)
	lineNo := 0
	emitted := 0
	for emitted < limit {
		line, more, err := readCappedLine(br, readMaxLineLen)
		if err != nil && line == nil {
			if err == io.EOF {
				break
			}
			return []Result{{Content: fmt.Sprintf("Error reading file: %v", err), IsError: true}}, nil
		}
		lineNo++
		if lineNo < start {
			continue
		}
		text := string(line)
		if more > 0 {
			text += fmt.Sprintf("… [line truncated: %d more bytes]", more)
		}
		// cat -n format: line number right-aligned in a 6-wide field, then a tab.
		fmt.Fprintf(&b, "%6d\t%s\n", lineNo, text)
		emitted++
		if err == io.EOF {
			break
		}
	}

	if emitted == 0 {
		return []Result{{Content: "<file is empty or offset is past end of file>"}}, nil
	}
	return []Result{{Content: b.String()}}, nil
}

// readCappedLine reads one line and returns at most max bytes of it, cut on a
// rune boundary, and how many bytes of the line were left out. The rest of a
// long line is read and discarded without being held.
//
// A bufio.Scanner with a 1 MB limit failed the whole read with "token too
// long" on a single long line (minified JavaScript, a data dump), and the
// byte slice line[:2000] could split a UTF-8 character.
func readCappedLine(br *bufio.Reader, max int) (line []byte, more int, err error) {
	total := 0
	for {
		chunk, rerr := br.ReadSlice('\n')
		if rerr == nil {
			chunk = chunk[:len(chunk)-1] // the delimiter itself
		}
		if room := max - len(line); room > 0 {
			line = append(line, chunk[:min(room, len(chunk))]...)
		}
		total += len(chunk)
		if rerr == bufio.ErrBufferFull {
			continue
		}
		if rerr == io.EOF && total == 0 {
			return nil, 0, io.EOF
		}
		err = rerr
		break
	}
	more = total - len(line)
	if more == 0 {
		line = bytes.TrimSuffix(line, []byte("\r"))
		return line, 0, err
	}
	for len(line) > 0 && !utf8.Valid(line) {
		line = line[:len(line)-1]
		more++
	}
	return line, more, err
}
