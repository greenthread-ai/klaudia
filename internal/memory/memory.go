// Package memory is a small Markdown-backed memory store. Session memory lives
// in .klaudia/MEMORY.md; detailed memory files live in .klaudia/memory/*.md;
// project knowledge lives in .klaudia/KNOWLEDGE.md.
package memory

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Store appends and reads memory notes from a MEMORY.md file in a given directory.
type Store struct {
	dir string
}

// New returns a Store rooted at dir (e.g. ".klaudia").
func New(dir string) *Store {
	return &Store{dir: dir}
}

// KnowledgePath returns the project KNOWLEDGE.md path for cwd.
func KnowledgePath(cwd string) string {
	return filepath.Join(cwd, ".klaudia", "KNOWLEDGE.md")
}

// AddKnowledge appends a timestamped bullet to .klaudia/KNOWLEDGE.md, creating
// .klaudia and a header on first write. Empty text is rejected.
func AddKnowledge(cwd, text string) error {
	return appendBullet(KnowledgePath(cwd), "# Project Knowledge\n\n", text)
}

// Path is the MEMORY.md file path.
func (s *Store) Path() string {
	return filepath.Join(s.dir, "MEMORY.md")
}

// Index returns the MEMORY.md contents, or "" if it does not exist yet.
func (s *Store) Index() (string, error) {
	contents, err := os.ReadFile(s.Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(contents), nil
}

// Entries returns the individual memory notes from MEMORY.md.
func (s *Store) Entries() ([]string, error) {
	contents, err := os.ReadFile(s.Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}

	var entries []string
	for _, line := range strings.Split(string(contents), "\n") {
		if entry, ok := strings.CutPrefix(line, "- "); ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// Search returns memory lines containing all whitespace-separated query terms.
// It searches the MEMORY.md index bullets and the content of each memory/*.md
// detail note; matches from a detail file are tagged "name.md: <line>" so the
// model knows which file to open for the full context. An empty query returns
// just the index entries (use FilePointers / Read to reach detail notes).
func (s *Store) Search(query string) ([]string, error) {
	entries, err := s.Entries()
	if err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return entries, nil
	}

	candidates := append([]string{}, entries...)

	// Detail notes: tag each content line with its filename so a hit points the
	// model at the right file.
	paths, _ := filepath.Glob(filepath.Join(s.dir, "memory", "*.md"))
	sort.Strings(paths)
	for _, path := range paths {
		name := filepath.Base(path)
		if name == "MEMORY.md" {
			continue
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(line)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			candidates = append(candidates, name+": "+t)
		}
	}

	var matches []string
	for _, c := range candidates {
		lower := strings.ToLower(c)
		matched := true
		for _, term := range terms {
			if !strings.Contains(lower, term) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, c)
		}
	}
	return matches, nil
}

// linkedSectionHeader titles the section of MEMORY.md that points at the
// memory/*.md detail notes.
const linkedSectionHeader = "## Linked memory"

// Add appends a timestamped bullet to MEMORY.md (creating the directory and a
// header on first write), then refreshes the linked-memory section. Empty
// (whitespace-only) text is rejected.
func (s *Store) Add(text string) error {
	if err := appendBullet(s.Path(), "# Memory\n\n", text); err != nil {
		return err
	}
	return s.SyncLinks()
}

// SyncLinks rewrites the "## Linked memory" section of MEMORY.md so it lists
// exactly the current memory/*.md detail notes, one pointer each (see
// FilePointers). It is idempotent — it only writes when the file would change —
// so calling it at startup and after each Add doesn't churn the file, and a
// removed or renamed note is reflected on the next sync. When no detail notes
// exist it strips any stale section; it never creates MEMORY.md just to hold an
// empty section.
func (s *Store) SyncLinks() error {
	pointers := s.FilePointers()

	existing, err := os.ReadFile(s.Path())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	body := stripLinkedSection(string(existing))

	updated := body
	if len(pointers) > 0 {
		if strings.TrimSpace(body) == "" {
			body = "# Memory\n\n" // a brand-new index needs a header
		}
		updated = strings.TrimRight(body, "\n") + "\n\n" + linkedSectionHeader + "\n\n" + strings.Join(pointers, "\n") + "\n"
	}

	if updated == string(existing) {
		return nil // already current — no write
	}
	if strings.TrimSpace(updated) == "" {
		return nil // nothing to write and nothing meaningful was there
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.Path(), []byte(updated), 0o644)
}

// stripLinkedSection removes the "## Linked memory" section (its header through
// the line before the next "## " heading, or end of file) from content,
// returning the rest unchanged. Content without the section is returned as-is.
func stripLinkedSection(content string) string {
	lines := strings.Split(content, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == linkedSectionHeader {
			start = i
			break
		}
	}
	if start == -1 {
		return content
	}
	end := len(lines)
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(strings.TrimSpace(lines[j]), "## ") {
			end = j
			break
		}
	}
	kept := append(append([]string{}, lines[:start]...), lines[end:]...)
	return strings.Join(kept, "\n")
}

// FilePointers returns one Markdown pointer line per detail note under
// memory/*.md, as "- [name](memory/name.md) — <hook>", where <hook> is the
// file's first heading or non-empty line. MEMORY.md itself is skipped.
//
// These let the index *reference* detail notes rather than inline them: recall
// stays cheap as memory grows, and the model opens a file (via Read or a Memory
// search) only when it's relevant.
func (s *Store) FilePointers() []string {
	paths, err := filepath.Glob(filepath.Join(s.dir, "memory", "*.md"))
	if err != nil {
		return nil
	}
	sort.Strings(paths)

	ptrs := make([]string, 0, len(paths))
	for _, path := range paths {
		name := filepath.Base(path)
		if name == "MEMORY.md" {
			continue
		}
		slug := strings.TrimSuffix(name, filepath.Ext(name))
		line := "- [" + slug + "](memory/" + name + ")"
		if hook := fileHook(path); hook != "" {
			line += " — " + hook
		}
		ptrs = append(ptrs, line)
	}
	return ptrs
}

// fileHook returns a one-line summary of a memory file: its first Markdown
// heading (with leading '#'s stripped) or, failing that, its first non-empty
// line. Returns "" if the file is unreadable or empty.
func fileHook(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		t = strings.TrimSpace(strings.TrimLeft(t, "#"))
		if len(t) > 80 {
			t = t[:80] + "…"
		}
		return t
	}
	return ""
}

func appendBullet(path, header, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("memory text is empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	_, statErr := os.Stat(path)
	newFile := errors.Is(statErr, os.ErrNotExist)
	if statErr != nil && !newFile {
		return statErr
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if newFile {
		if _, err := file.WriteString(header); err != nil {
			return err
		}
	}

	_, err = file.WriteString("- " + time.Now().Format(time.RFC3339) + " " + text + "\n")
	return err
}
