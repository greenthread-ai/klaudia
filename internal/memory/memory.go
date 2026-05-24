// Package memory is a small session memory store backed by a MEMORY.md
// Markdown file. It backs the /memory command: viewing recalled notes and
// adding new ones. (Dogfood: the core was implemented by GPT-5.5 running in
// Klaudia.)
package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store appends and reads memory notes from a MEMORY.md under a directory.
type Store struct {
	dir string
}

// New returns a Store rooted at dir (e.g. ".klaudia/memory").
func New(dir string) *Store {
	return &Store{dir: dir}
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
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("memory text is empty")
	}

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}

	path := s.Path()
	_, err := os.Stat(path)
	newFile := errors.Is(err, os.ErrNotExist)
	if err != nil && !newFile {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if newFile {
		if _, err := file.WriteString("# Memory\n\n"); err != nil {
			return err
		}
	}

	_, err = file.WriteString("- " + time.Now().Format(time.RFC3339) + " " + text + "\n")
	return err
}
