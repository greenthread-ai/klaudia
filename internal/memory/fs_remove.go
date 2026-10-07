package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Remove deletes the one session bullet in MEMORY.md whose entry matches every
// term of query. See Store.Remove for the contract.
//
// Only bullets outside the "## Linked memory" section are candidates: the
// pointers there are rebuilt from memory/*.md by SyncLinks, so removing one
// would not stick. Refusing an ambiguous query, rather than removing every
// match, keeps a loose query from wiping notes the caller did not mean; the
// entry's timestamp is part of what is matched, so any single bullet can be
// named exactly.
func (s *fsStore) Remove(query string) ([]string, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, ErrEmpty
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var hits []int
	var matched []string
	inLinked := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case t == linkedSectionHeader:
			inLinked = true
			continue
		case inLinked && strings.HasPrefix(t, "## "):
			inLinked = false // same section bounds as stripLinkedSection
		}
		if inLinked {
			continue
		}
		entry, ok := strings.CutPrefix(line, "- ")
		if !ok || !containsAllTerms(strings.ToLower(entry), terms) {
			continue
		}
		hits = append(hits, i)
		matched = append(matched, entry)
	}

	switch len(hits) {
	case 0:
		return nil, ErrNotFound
	case 1:
	default:
		return matched, ErrAmbiguous
	}
	at := hits[0]
	kept := append(append([]string{}, lines[:at]...), lines[at+1:]...)
	if err := os.WriteFile(s.Path(), []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return nil, err
	}
	return matched, nil
}

// RemoveNote deletes memory/<name>.md and refreshes the linked-memory section
// so the index stops pointing at it. A trailing ".md" on name is accepted.
func (s *fsStore) RemoveNote(name string) error {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".md")
	if name == "" || name == "." || name == ".." || name == "MEMORY" ||
		strings.ContainsAny(name, `/\`) {
		return ErrInvalidName
	}
	path := filepath.Join(s.dir, "memory", name+".md")
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return s.SyncLinks()
}

// containsAllTerms reports whether lower contains every term. Both are
// expected in lower case.
func containsAllTerms(lower string, terms []string) bool {
	for _, term := range terms {
		if !strings.Contains(lower, term) {
			return false
		}
	}
	return true
}
