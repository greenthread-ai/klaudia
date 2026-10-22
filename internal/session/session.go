// Package session reads and writes Klaudia session transcripts:
// newline-delimited JSON (JSONL) under ~/.klaudia/sessions/<encoded-cwd>/.
package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

	// Resolved model sizing, assistant-only. These five have no counterpart in
	// the JS schema — they are a deliberate Klaudia divergence from the tag
	// parity the doc comment above claims.
	//
	// They exist because a transcript recorded everything about a turn except
	// the three numbers that decide what the turn can do. When Opus 5.5 ran at
	// max_tokens=8192 against a real cap of 128000, the saved session named the
	// permission mode, the version, the git branch and the request id, and not
	// the model, the cap or the window; the diagnosis came from the user
	// remembering which model they had picked. Recording the figures and the
	// LimitSource* that produced each one makes that answerable from the file.
	//
	// Assistant-only for the same reason as RequestID: they describe what
	// produced this content, and a user line was not produced by a model.
	// Repeating them there would double the bytes per turn for no new fact.
	Model               string `json:"model,omitempty"`
	MaxTokens           int    `json:"maxTokens,omitempty"`
	MaxTokensSource     string `json:"maxTokensSource,omitempty"`
	ContextWindow       int    `json:"contextWindow,omitempty"`
	ContextWindowSource string `json:"contextWindowSource,omitempty"`

	// system-only: a compaction boundary ("compact_boundary", Claude Code's
	// name for the same line). Content is the summary that replaced every
	// entry above it. Read skips system lines, so existing readers never see
	// one; ReadForResume is the reader that does.
	Subtype string `json:"subtype,omitempty"`
	Content string `json:"content,omitempty"`
}

// SubtypeCompactBoundary marks the point where history was replaced by a
// summary.
const SubtypeCompactBoundary = "compact_boundary"

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

// ResumePoint is what --resume rebuilds a conversation from.
type ResumePoint struct {
	// Summary is the last compaction's summary, "" when the transcript was
	// never compacted (or predates boundary lines).
	Summary string
	// After holds the user/assistant entries recorded after that compaction —
	// all of them when there was none.
	After []Entry
}

// ReadForResume reads a transcript the way the live conversation stood when
// it was last written: the last compaction's summary, then every entry after
// it.
//
// --resume used to seed from the .summary.md file alone and drop the rest, so
// everything said after the most recent compaction was lost — measured on one
// restart as 572 entries and 80 minutes of work, which the resumed session
// then could not remember doing.
func ReadForResume(path string) (ResumePoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return ResumePoint{}, err
	}
	defer f.Close()

	var rp ResumePoint
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		switch {
		case e.Type == "system" && e.Subtype == SubtypeCompactBoundary && strings.TrimSpace(e.Content) != "":
			rp.Summary, rp.After = e.Content, nil
		case e.Type == "user" || e.Type == "assistant":
			rp.After = append(rp.After, e)
		}
	}
	return rp, sc.Err()
}

// EntriesAfter returns the entries timestamped strictly after ts. It is the
// fallback for transcripts written before boundary lines existed, cut at the
// summary file's "_Compacted <ts>_" stamp. Timestamps are all Now()'s
// fixed-width UTC form, so string order is time order.
func EntriesAfter(entries []Entry, ts string) []Entry {
	for i, e := range entries {
		if e.Timestamp > ts {
			return entries[i:]
		}
	}
	return nil
}

// MostRecent returns the session ID of the most recently modified transcript in
// the project dir for cwd, or ("", false) if none exists. The current sessions
// root and legacy projects root are both considered during migration.
//
// List already does the work — newest first, contentless files dropped — and
// "most recent" is its first element. Keeping a second traversal here is how
// the two previously disagreed about what counted as a session.
func MostRecent(cwd string) (string, bool) {
	all := List(cwd)
	if len(all) == 0 {
		return "", false
	}
	return all[0].ID, true
}

// LastTurnRefused reports whether the transcript's final assistant turn was a
// model refusal.
//
// A refusal is a dead end for resume: the safety system is tripped by the
// accumulated context, not the last message, so resuming the conversation makes
// the very next prompt refuse too — even an unrelated one. Auto-resume uses this
// to start fresh instead of reviving a session that can only refuse.
//
// Two signatures, covering transcripts written before and after the recorder
// stored request-shape messages: an explicit stop_reason of "refusal" (older
// response-shape entries keep it), or an assistant turn with no content at all,
// which is the only way a refusal is persisted once stop_reason is gone. A
// normal turn always carries text or a tool call; an interrupted turn is not
// recorded. So an empty trailing assistant turn is a refusal in practice.
func LastTurnRefused(entries []Entry) bool {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type != "assistant" {
			continue
		}
		var m struct {
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(entries[i].Message, &m); err != nil {
			return false // unreadable: don't guess it's a refusal
		}
		if m.StopReason == "refusal" {
			return true
		}
		return !hasUsableContent(m.Content)
	}
	return false
}

func hasUsableContent(content []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) bool {
	for _, b := range content {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				return true
			}
		case "tool_use", "server_tool_use":
			return true
		}
	}
	return false
}
