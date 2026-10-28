package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Info describes one persisted transcript, enough to show it in a picker
// without reading the conversation back.
type Info struct {
	ID       string
	Path     string
	Modified time.Time
	// Title is the session's first user prompt flattened to one line, or "" when
	// the transcript opens with an assistant turn (a replayed summary) or the
	// prompt was not text.
	Title string
}

// maxTitle bounds Info.Title. A prompt can be a whole pasted file; a picker
// entry cannot.
const maxTitle = 200

// List returns the transcripts for cwd that hold a conversation, newest first.
//
// Contentless files are skipped for the same reason MostRecent skips them: a
// launch creates its transcript eagerly but only records on a real turn, so an
// abandoned session leaves a zero-message file with the newest mtime. Offering
// that as something to resume is offering an empty conversation.
func List(cwd string) []Info {
	matches, _ := filepath.Glob(filepath.Join(Dir(cwd), "*.jsonl"))
	legacy, _ := filepath.Glob(filepath.Join(legacyDir(cwd), "*.jsonl"))
	matches = append(matches, legacy...)

	out := make([]Info, 0, len(matches))
	seen := map[string]bool{}
	for _, path := range matches {
		st, err := os.Stat(path)
		if err != nil || st.Size() == 0 {
			continue
		}
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		// The current and legacy roots can both hold the same id mid-migration.
		// The current root is globbed first, so the first one wins.
		if seen[id] {
			continue
		}
		title, from, ok := firstPrompt(path)
		if !ok {
			continue
		}
		// EncodePath maps every non-alphanumeric character to "-", so
		// /a/my_proj, /a/my-proj and /a/my.proj share one directory, and
		// auto-resume picked up another folder's conversation. A transcript
		// whose first message was recorded elsewhere is not this folder's;
		// one with no recorded cwd (older transcripts, imports) is accepted.
		// The first message is the one to check: a session resumed by id
		// from elsewhere appends to its original file. (From PR #25.)
		if from != "" && !sameDir(from, cwd) {
			continue
		}
		seen[id] = true
		out = append(out, Info{ID: id, Path: path, Modified: st.ModTime(), Title: title})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}

// sameDir reports whether two paths name one directory, through symlinks too:
// a folder reached by a different spelling (/var and /private/var on macOS)
// must not hide its own sessions.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// firstPrompt scans a transcript up to its first message line and returns that
// line's text if it is a user turn, and the cwd it was recorded in. ok reports
// whether any message was found at all, which is this file's answer to "does
// this transcript hold a conversation".
//
// It stops at the first message rather than reading the file: transcripts run to
// megabytes, and List is called for every session in the project.
func firstPrompt(path string) (title, cwd string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	_ = eachLine(f, func(line []byte) bool {
		var e struct {
			Type    string          `json:"type"`
			CWD     string          `json:"cwd"`
			Message json.RawMessage `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil {
			return true
		}
		switch e.Type {
		case "user":
			title, cwd, ok = messageTitle(e.Message), e.CWD, true
			return false
		case "assistant":
			cwd, ok = e.CWD, true
			return false
		}
		return true
	})
	return title, cwd, ok
}

// messageTitle pulls display text out of a recorded Anthropic message. Content
// is either a bare string or a block list, and both shapes are on disk: the
// recorder stores whatever the API took.
func messageTitle(raw json.RawMessage) string {
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return oneline(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return oneline(b.Text)
		}
	}
	return ""
}

func oneline(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxTitle {
		s = string(r[:maxTitle-1]) + "…"
	}
	return s
}
