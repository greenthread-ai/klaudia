// Package session reads and writes Klaudia session transcripts:
// newline-delimited JSON (JSONL) under ~/.klaudia/sessions/<encoded-cwd>/.
package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// maxDirLen is the project-dir name length cap before truncation+hash (q0A=200).
const maxDirLen = 200

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

// ConfigRoot returns ~/.klaudia (honoring KLAUDIA_CONFIG_DIR).
func ConfigRoot() string {
	base := os.Getenv("KLAUDIA_CONFIG_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".klaudia")
	}
	return base
}

// SessionsRoot returns ~/.klaudia/sessions (honoring KLAUDIA_CONFIG_DIR).
func SessionsRoot() string {
	return filepath.Join(ConfigRoot(), "sessions")
}

func legacyProjectsRoot() string {
	return filepath.Join(ConfigRoot(), "projects")
}

// EncodePath maps an absolute path to a project-dir name, matching K0A
// (03-providers.js:362): non-alphanumerics become "-"; if the result exceeds
// 200 chars it is truncated and a 32-bit string-hash suffix (base36) is added.
func EncodePath(path string) string {
	enc := nonAlnum.ReplaceAllString(path, "-")
	if len(enc) <= maxDirLen {
		return enc
	}
	var h int32
	for _, r := range path {
		h = (h << 5) - h + int32(r) // h = 31*h + c, with int32 overflow
	}
	abs := int64(h)
	if abs < 0 {
		abs = -abs
	}
	return enc[:maxDirLen] + "-" + strconv.FormatInt(abs, 36)
}

// Dir returns the project transcript directory for a working directory.
func Dir(cwd string) string {
	return filepath.Join(SessionsRoot(), EncodePath(cwd))
}

func legacyDir(cwd string) string {
	return filepath.Join(legacyProjectsRoot(), EncodePath(cwd))
}

// Path returns the transcript file path for a session in a working directory.
func Path(cwd, sessionID string) string {
	return filepath.Join(Dir(cwd), sessionID+".jsonl")
}

// ExistingPath returns the transcript file path for a session. It prefers the
// newer copy when both the current sessions root and legacy projects root have
// the same session ID.
func ExistingPath(cwd, sessionID string) string {
	path := Path(cwd, sessionID)
	legacy := filepath.Join(legacyDir(cwd), sessionID+".jsonl")
	st, err := os.Stat(path)
	legacySt, legacyErr := os.Stat(legacy)
	switch {
	case err == nil && legacyErr == nil && legacySt.ModTime().After(st.ModTime()):
		return legacy
	case err == nil:
		return path
	case legacyErr == nil:
		return legacy
	default:
		return path
	}
}

// Entry is one transcript line. Field names/tags match the JS schema; optional
// fields are omitted when empty so output stays close to the reference.
type Entry struct {
	Type        string          `json:"type"` // "user" | "assistant"
	UUID        string          `json:"uuid"`
	ParentUUID  *string         `json:"parentUuid"`
	SessionID   string          `json:"sessionId"`
	Timestamp   string          `json:"timestamp"`
	CWD         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch,omitempty"`
	IsSidechain bool            `json:"isSidechain"`
	UserType    string          `json:"userType"`
	Version     string          `json:"version"`
	Message     json.RawMessage `json:"message"`

	// user-only
	PermissionMode string `json:"permissionMode,omitempty"`
	// assistant-only
	RequestID string `json:"requestId,omitempty"`
}

// Now returns an RFC3339 millisecond UTC timestamp matching the JS format
// (e.g. "2026-05-24T03:58:31.020Z").
func Now() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// Writer appends entries to a session transcript file. The directory and file
// are created lazily on the first Append, so a session that records nothing — a
// launch quit before any turn, or one whose turns all errored (e.g. an expired
// auth token) — leaves no empty transcript behind to clutter the sessions dir
// or shadow auto-resume.
type Writer struct {
	path string
	f    *os.File
}

// NewWriter prepares a transcript writer for the session. It performs no I/O:
// the parent dir and file are created on the first Append, so merely
// constructing a writer (and never appending) never leaves a file on disk.
func NewWriter(cwd, sessionID string) (*Writer, error) {
	return &Writer{path: Path(cwd, sessionID)}, nil
}

// open creates the parent dir and opens the file for append on first use.
func (w *Writer) open() error {
	if w.f != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	w.f = f
	return nil
}

// Append writes one entry as a JSON line, creating the file on first call.
func (w *Writer) Append(e Entry) error {
	if err := w.open(); err != nil {
		return err
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.f.Write(b)
	return err
}

// Path returns the transcript file path (whether or not it has been created).
func (w *Writer) Path() string { return w.path }

// Close closes the underlying file. A writer that never appended is a no-op and
// leaves no file behind.
func (w *Writer) Close() error {
	if w.f == nil {
		return nil
	}
	return w.f.Close()
}

// Read parses all user/assistant entries from a transcript file, in order.
// Non-message lines (queue-operation, ai-title, attachment, etc.) are skipped.
func Read(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // tolerate malformed/unknown lines
		}
		if e.Type == "user" || e.Type == "assistant" {
			entries = append(entries, e)
		}
	}
	return entries, sc.Err()
}

// MostRecent returns the session ID of the most recently modified transcript in
// the project dir for cwd, or ("", false) if none exists. The current sessions
// root and legacy projects root are both considered during migration.
func MostRecent(cwd string) (string, bool) {
	matches, _ := filepath.Glob(filepath.Join(Dir(cwd), "*.jsonl"))
	legacyMatches, _ := filepath.Glob(filepath.Join(legacyDir(cwd), "*.jsonl"))
	matches = append(matches, legacyMatches...)
	if len(matches) == 0 {
		return "", false
	}
	type fi struct {
		path string
		mod  time.Time
	}
	var files []fi
	for _, m := range matches {
		st, err := os.Stat(m)
		if err != nil || st.Size() == 0 {
			continue // unreadable, or an empty file from a launch that recorded nothing
		}
		files = append(files, fi{path: m, mod: st.ModTime()})
	}
	if len(files) == 0 {
		return "", false
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	// Return the newest transcript that actually holds a conversation. A launch
	// creates its transcript eagerly (O_CREATE) but only records on a real turn,
	// so a session abandoned before any message — or one whose turns all errored
	// (e.g. an expired auth token) — leaves a contentless file with the newest
	// mtime. Auto-resuming that would silently drop the user's real history
	// ("resumed but no memory"), so skip to the newest one with messages.
	for _, f := range files {
		if hasMessages(f.path) {
			name := filepath.Base(f.path)
			return strings.TrimSuffix(name, ".jsonl"), true
		}
	}
	return "", false
}

// hasMessages reports whether a transcript holds at least one user/assistant
// message, scanning only until the first match so large transcripts stay cheap.
// A file with only non-message lines (or none) is treated as contentless.
func hasMessages(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var e struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(sc.Bytes(), &e) == nil && (e.Type == "user" || e.Type == "assistant") {
			return true
		}
	}
	return false
}
