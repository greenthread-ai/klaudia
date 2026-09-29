package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// promptHistoryFile is the per-project ↑ history, beside the project's
// transcripts. Not ".jsonl": MostRecent and Locate treat every *.jsonl in the
// project dir as a transcript, and a history file must never be resumable as a
// session.
const promptHistoryFile = "prompt-history.ndjson"

// PromptHistoryPath returns the prompt-history file for a working directory.
//
// It lives in the sessions dir rather than the project's .klaudia/ because a
// prompt is as private as the transcript it came from: .klaudia/ holds
// settings and skills a team commits, and a history file there is one
// `git add .klaudia` away from publishing every prompt typed in the repo. The
// sessions dir is already per-project, already outside the tree, and already
// holds the same text in the transcripts.
func PromptHistoryPath(cwd string) string {
	return filepath.Join(Dir(cwd), promptHistoryFile)
}

// PromptHistory is an append-only file of submitted prompts, one JSON string
// per line (so a multi-line prompt stays one entry).
//
// Appends are single O_APPEND writes, so two Klaudia sessions in the same
// project interleave their lines rather than overwrite each other's. The file
// is trimmed back to the newest limit entries when it has grown to twice that,
// which keeps the rewrite (the one racy step) rare.
type PromptHistory struct {
	path  string
	max   int
	lines int // lines on disk as far as this process knows, for trimming
}

// NewPromptHistory returns a history backed by path, keeping at most limit
// entries.
func NewPromptHistory(path string, limit int) *PromptHistory {
	return &PromptHistory{path: path, max: limit}
}

// Load returns the stored prompts, oldest first, at most limit of them. A
// missing file is an empty history, not an error. Lines that do not decode are
// skipped: one torn write should not cost the rest of the history.
func (h *PromptHistory) Load() ([]string, error) {
	entries, lines, err := h.read()
	if err != nil {
		return nil, err
	}
	h.lines = lines
	if len(entries) > h.max {
		entries = entries[len(entries)-h.max:]
	}
	if lines > h.max {
		// Best effort: an untrimmed file still loads correctly.
		if h.rewrite(entries) == nil {
			h.lines = len(entries)
		}
	}
	return entries, nil
}

// Append records one prompt.
func (h *PromptHistory) Append(entry string) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	if cerr != nil {
		return cerr
	}
	h.lines++
	if h.lines >= 2*h.max {
		if _, err := h.Load(); err != nil {
			return err
		}
	}
	return nil
}

// read decodes the file, dropping an entry that repeats the one before it
// (two sessions submitting the same line, or a crash between write and trim).
func (h *PromptHistory) read() (entries []string, lines int, err error) {
	f, err := os.Open(h.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		lines++
		var s string
		if json.Unmarshal([]byte(raw), &s) != nil || s == "" {
			continue
		}
		if n := len(entries); n > 0 && entries[n-1] == s {
			continue
		}
		entries = append(entries, s)
	}
	return entries, lines, sc.Err()
}

// rewrite replaces the file with entries, via a temp file and rename so a
// reader never sees it half-written.
func (h *PromptHistory) rewrite(entries []string) error {
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".prompt-history-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(b.String())
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o600)
	}
	if werr == nil {
		werr = os.Rename(name, h.path)
	}
	if werr != nil {
		_ = os.Remove(name)
	}
	return werr
}
