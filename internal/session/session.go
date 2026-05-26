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

// Writer appends entries to a session transcript file.
type Writer struct {
	path string
	f    *os.File
}

// NewWriter opens (creating dirs and the file) the transcript for append.
func NewWriter(cwd, sessionID string) (*Writer, error) {
	path := Path(cwd, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{path: path, f: f}, nil
}

// Append writes one entry as a JSON line.
func (w *Writer) Append(e Entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.f.Write(b)
	return err
}

// Path returns the transcript file path.
func (w *Writer) Path() string { return w.path }

// Close closes the underlying file.
func (w *Writer) Close() error { return w.f.Close() }

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
		if err != nil {
			continue
		}
		files = append(files, fi{path: m, mod: st.ModTime()})
	}
	if len(files) == 0 {
		return "", false
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	name := filepath.Base(files[0].path)
	return strings.TrimSuffix(name, ".jsonl"), true
}
