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

// Search returns entries containing all whitespace-separated query terms.
func (s *Store) Search(query string) ([]string, error) {
	entries, err := s.Entries()
	if err != nil {
		return nil, err
	}

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return entries, nil
	}

	var matches []string
	for _, entry := range entries {
		entryLower := strings.ToLower(entry)
		matched := true
		for _, term := range terms {
			if !strings.Contains(entryLower, term) {
				matched = false
				break
			}
		}
		if matched {
			matches = append(matches, entry)
		}
	}
	return matches, nil
}

// Add appends a timestamped bullet to MEMORY.md, creating the directory and a
// header on first write. Empty (whitespace-only) text is rejected.
func (s *Store) Add(text string) error {
	return appendBullet(s.Path(), "# Memory\n\n", text, s.linkedMemoryFiles()...)
}

func (s *Store) linkedMemoryFiles() []string {
	paths, err := filepath.Glob(filepath.Join(s.dir, "memory", "*.md"))
	if err != nil || len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)

	links := make([]string, 0, len(paths))
	for _, path := range paths {
		name := filepath.Base(path)
		if name == "MEMORY.md" {
			continue
		}
		links = append(links, "["+strings.TrimSuffix(name, filepath.Ext(name))+"](memory/"+name+")")
	}
	return links
}

func appendBullet(path, header, text string, links ...string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("memory text is empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	existing, readErr := os.ReadFile(path)
	newFile := errors.Is(readErr, os.ErrNotExist)
	if readErr != nil && !newFile {
		return readErr
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
	if err := appendMissingLinks(file, string(existing), links); err != nil {
		return err
	}

	_, err = file.WriteString("- " + time.Now().Format(time.RFC3339) + " " + text + "\n")
	return err
}

func appendMissingLinks(file *os.File, existing string, links []string) error {
	var missing []string
	for _, link := range links {
		if !strings.Contains(existing, link) {
			missing = append(missing, link)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if strings.TrimSpace(existing) != "" && !strings.HasSuffix(existing, "\n") {
		if _, err := file.WriteString("\n"); err != nil {
			return err
		}
	}
	if _, err := file.WriteString("\n## Linked memory\n"); err != nil {
		return err
	}
	for _, link := range missing {
		if _, err := file.WriteString("- " + link + "\n"); err != nil {
			return err
		}
	}
	return nil
}
