package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/greenthread-ai/klaudia/internal/tools"
)

// @file expansion happens at submit time, not while typing: the on-screen
// prompt keeps the short "@path" token (the same way a big paste stays a chip,
// see paste.go), and only the payload the model receives is expanded. A
// reference the user completed with Tab (completeAtPath) therefore arrives at
// the model as the file's actual contents, not as a string the model would have
// to go and Read for itself.
//
// Syntax (documented in docs/ux-spec.md):
//
//	@path            — the whole file
//	@path:START-END  — lines START..END, 1-based and inclusive
//	@path:LINE       — a single line (START-END with START==END)
//
// The colon form is the one compilers and stack traces already print, so it is
// what a pasted "file.go:42" completes to; a range just adds the "-END" half.
// A text file is inlined behind a clear delimiter; an image file (png / jpeg /
// gif / webp) becomes a base64 image block on the user message instead of being
// dumped as bytes; a non-image binary is refused with a short note. Nothing is
// silently truncated — an oversized file is refused whole, so the model never
// reasons about half a file it was not told was half.

const (
	// atFileMaxBytes bounds an inlined text file. A quarter-megabyte is already
	// several thousand lines; past that the intent is almost always "look at
	// this file" (a Read), not "carry all of it in every request".
	atFileMaxBytes = 256 << 10
	// atImageMaxBytes bounds a base64-attached image. The API rejects very large
	// images anyway; refusing early keeps a stray @video.webm-sized file from
	// ballooning the request.
	atImageMaxBytes = 5 << 20
	// atBinarySniff is how many leading bytes decide text-vs-binary.
	atBinarySniff = 8000
)

// atResult is what expandAtFiles produces: the expanded prompt text, any image
// attachments to hang on the user message, and human-readable notes about
// references that could not be expanded (missing / oversized / binary). Notes
// are for the user's terminal, never sent to the model.
type atResult struct {
	Prompt string
	Images []tools.ResultImage
	Notes  []string
}

// imageMediaTypes maps an extension to the media type the API expects. A file
// whose extension is not here is treated as text (or, if it sniffs binary,
// refused): we never guess an image type from contents.
var imageMediaTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// expandAtFiles rewrites @-references in text against root. Tokens that do not
// resolve to a real file are left exactly as typed: "@nobody" in prose, an
// email, or a decorator must survive untouched, so existence is the gate (the
// same disambiguator parseFileRef uses). A token that *looks* like a path —
// it has a slash or a file extension — but does not exist earns a note, because
// there the user plainly meant a file and mistyped it.
func expandAtFiles(text, root string) atResult {
	res := atResult{Prompt: text}
	var sections []string
	seen := map[string]bool{}
	for _, tok := range scanAtTokens(text) {
		path, start, end := splitLineRange(tok)
		if path == "" {
			continue
		}
		full := path
		if !filepath.IsAbs(path) && root != "" {
			full = filepath.Join(root, path)
		}
		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			if looksLikePath(path) {
				res.Notes = append(res.Notes, fmt.Sprintf("@%s: no such file", tok))
			}
			continue
		}
		// A path referenced twice (or a plain @path and @path:1-3) is expanded
		// once; the second mention still reads fine against the one section.
		if seen[path+":"+strconv.Itoa(start)+":"+strconv.Itoa(end)] {
			continue
		}
		seen[path+":"+strconv.Itoa(start)+":"+strconv.Itoa(end)] = true

		ext := strings.ToLower(filepath.Ext(path))
		if media, ok := imageMediaTypes[ext]; ok {
			if start != 0 || end != 0 {
				res.Notes = append(res.Notes, fmt.Sprintf("@%s: line ranges do not apply to images; attaching the whole image", tok))
			}
			if info.Size() > atImageMaxBytes {
				res.Notes = append(res.Notes, fmt.Sprintf("@%s: image is %s, over the %s limit; not attached", path, atBytes(info.Size()), atBytes(atImageMaxBytes)))
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				res.Notes = append(res.Notes, fmt.Sprintf("@%s: %v", path, err))
				continue
			}
			res.Images = append(res.Images, tools.ResultImage{
				MediaType: media,
				Base64:    base64.StdEncoding.EncodeToString(data),
			})
			continue
		}

		// Text file. A whole-file reference over the cap is refused before it is
		// read; a ranged reference is allowed to read a large file because the
		// slice it keeps is what's bounded.
		if start == 0 && end == 0 && info.Size() > atFileMaxBytes {
			res.Notes = append(res.Notes, fmt.Sprintf("@%s: file is %s, over the %s limit; reference a line range or Read it instead", path, atBytes(info.Size()), atBytes(atFileMaxBytes)))
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("@%s: %v", path, err))
			continue
		}
		if looksBinary(data) {
			res.Notes = append(res.Notes, fmt.Sprintf("@%s: looks like a binary file; not attached", path))
			continue
		}
		body := string(data)
		label := path
		if start != 0 || end != 0 {
			sliced, n, ok := sliceLines(body, start, end)
			if !ok {
				res.Notes = append(res.Notes, fmt.Sprintf("@%s: lines %d-%d are outside the file (%d line(s))", path, start, end, n))
				continue
			}
			body, label = sliced, fmt.Sprintf("%s:%d-%d", path, start, end)
		}
		if len(body) > atFileMaxBytes {
			res.Notes = append(res.Notes, fmt.Sprintf("@%s: selection is %s, over the %s limit; narrow the range", label, atBytes(int64(len(body))), atBytes(atFileMaxBytes)))
			continue
		}
		sections = append(sections, fmt.Sprintf("===== %s =====\n%s", label, body))
	}
	if len(sections) > 0 {
		res.Prompt = strings.TrimRight(text, "\n") + "\n\n" + strings.Join(sections, "\n\n")
	}
	return res
}

// atBytes formats a byte count for a note, delegating to the package's shared
// humanBytes (clipboard.go).
func atBytes(n int64) string { return humanBytes(int(n)) }

// scanAtTokens returns the body (everything after the leading '@', trailing
// prose punctuation trimmed) of every @-token in s. A token starts at an '@'
// that opens a word — at the start of the string or after whitespace or an
// opening bracket/quote — and runs to the next whitespace. Anything else (an
// email's "@", a "foo@bar" handle mid-word) is not a reference and is skipped.
func scanAtTokens(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '@' {
			continue
		}
		if i > 0 {
			prev, _ := utf8.DecodeLastRuneInString(s[:i])
			if !unicode.IsSpace(prev) && !strings.ContainsRune("([{<\"'`", prev) {
				continue
			}
		}
		j := i + 1
		for j < len(s) {
			r, size := utf8.DecodeRuneInString(s[j:])
			if unicode.IsSpace(r) {
				break
			}
			j += size
		}
		body := strings.TrimRight(s[i+1:j], ",;.!?)]}>\"'`")
		if body != "" {
			out = append(out, body)
		}
		i = j
	}
	return out
}

// splitLineRange peels a trailing ":N" or ":N-M" off a token. A suffix that is
// not a positive range (a Windows drive letter, a "path:" with no number) is
// left as part of the path, so the reference still resolves.
func splitLineRange(s string) (path string, start, end int) {
	idx := strings.LastIndexByte(s, ':')
	if idx <= 0 || idx == len(s)-1 {
		return s, 0, 0
	}
	if a, b, ok := parseRange(s[idx+1:]); ok {
		return s[:idx], a, b
	}
	return s, 0, 0
}

// parseRange reads "N" (a single line) or "N-M" (a range) of positive,
// ascending line numbers.
func parseRange(spec string) (start, end int, ok bool) {
	if dash := strings.IndexByte(spec, '-'); dash >= 0 {
		a, err1 := strconv.Atoi(spec[:dash])
		b, err2 := strconv.Atoi(spec[dash+1:])
		if err1 != nil || err2 != nil || a <= 0 || b <= 0 || b < a {
			return 0, 0, false
		}
		return a, b, true
	}
	n, err := strconv.Atoi(spec)
	if err != nil || n <= 0 {
		return 0, 0, false
	}
	return n, n, true
}

// sliceLines returns lines start..end (1-based, inclusive) of body, the total
// line count, and whether the range overlaps the file at all. A start past the
// end fails; an end past the end is clamped, so "@f:1-9999" gives the whole file
// rather than an error.
func sliceLines(body string, start, end int) (string, int, bool) {
	lines := strings.Split(body, "\n")
	// A trailing newline yields an empty final element that is not a line.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	n := len(lines)
	if start < 1 || start > n {
		return "", n, false
	}
	if end > n {
		end = n
	}
	return strings.Join(lines[start-1:end], "\n"), n, true
}

// looksLikePath reports whether a token the user wrote was plainly meant as a
// file path — it has a directory separator or a file extension — as opposed to
// a bare "@handle" that merely failed to resolve.
func looksLikePath(s string) bool {
	return strings.ContainsRune(s, '/') || filepath.Ext(s) != ""
}

// looksBinary reports whether data is more likely bytes than text: a NUL in the
// sniff window, or invalid UTF-8. Read-of-image handles real images; this is the
// backstop for "@a.out" and friends so their bytes never land in the prompt.
func looksBinary(data []byte) bool {
	if len(data) > atBinarySniff {
		data = data[:atBinarySniff]
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}
