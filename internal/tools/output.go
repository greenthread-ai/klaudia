package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/greenthread-ai/klaudia/internal/session"
)

// Bash output has to be capped — a 5 MB build log would swallow the context
// window — but *where* it is cut matters more than the size of the cap.
//
// Cutting only the head is the worst choice available, because the part of a
// command's output people actually want is almost always at the end: the FAIL
// summary from `go test ./...`, the error that stopped a build, the last line
// of a stack trace. Keeping a head and a tail costs the same number of bytes
// and keeps both the invocation context and the verdict.
//
// The full text is also written to disk, so nothing is truly lost: the notice
// names the file, which means the model can grep it and /last can page it.

const (
	// bashMaxOutput caps combined output to protect the context window.
	bashMaxOutput = 30000
	// The cap is split head-heavy: the beginning carries what ran and how it
	// started, but the tail is where the verdict is, so it gets a third.
	bashHeadBytes = 20000
	bashTailBytes = bashMaxOutput - bashHeadBytes

	// spillMaxAge is how long full-output files are kept before being pruned.
	spillMaxAge = 24 * time.Hour
)

// clampOutput trims out to the head+tail budget. It returns the trimmed text
// and the number of bytes removed from the middle (0 when nothing was cut).
func clampOutput(out string) (string, int) {
	if len(out) <= bashMaxOutput {
		return out, 0
	}
	head := cutAfterLastLine(out[:bashHeadBytes])
	tail := cutBeforeFirstLine(out[len(out)-bashTailBytes:])
	elided := len(out) - len(head) - len(tail)
	return head + "\n" + elisionMarker(elided) + "\n" + tail, elided
}

func elisionMarker(n int) string {
	return fmt.Sprintf("... [%d bytes elided from the middle] ...", n)
}

// cutAfterLastLine trims back to the last complete line, so the head never ends
// mid-line. Newlines are ASCII and can't appear inside a multi-byte sequence, so
// this also guarantees a valid rune boundary; the fallback handles the
// pathological no-newline case.
func cutAfterLastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// cutBeforeFirstLine trims forward to the next line start, so the tail never
// begins mid-line.
func cutBeforeFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < len(s)-1 {
		return s[i+1:]
	}
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[1:]
	}
	return s
}

// spillDir is where full command output is kept.
func spillDir() string { return filepath.Join(session.ConfigRoot(), "outputs") }

// spillOutput writes the untruncated text to disk and returns its path, so the
// elided middle stays reachable. Failure is not an error worth surfacing — the
// caller just omits the path from the notice.
func spillOutput(full string) (string, bool) {
	dir := spillDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", false
	}
	pruneSpills(dir, spillMaxAge)

	f, err := os.CreateTemp(dir, "bash-*.log")
	if err != nil {
		return "", false
	}
	defer f.Close()
	if _, err := f.WriteString(full); err != nil {
		os.Remove(f.Name())
		return "", false
	}
	return f.Name(), true
}

// pruneSpills removes spill files older than maxAge. Called on each spill,
// which is cheap and keeps the directory from growing without bound.
func pruneSpills(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "bash-") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// spillMarker is the notice appended when full output was written to disk. It
// is exported through SpillPath so the format lives in exactly one place: the
// TUI parses it back out to offer the complete log.
const spillMarker = "[full output: "

// SpillPath extracts the full-output file path from clamped Bash output,
// reporting false when there isn't one or the file is gone.
func SpillPath(content string) (string, bool) {
	i := strings.LastIndex(content, spillMarker)
	if i < 0 {
		return "", false
	}
	rest := content[i+len(spillMarker):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return "", false
	}
	path := rest[:j]
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}
