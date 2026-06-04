package memory

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// detailEntries walks .klaudia/memory/*.md once and returns one Entry per
// note. Fields populated today: Name, Path, Title, Updated (mtime), Created
// (mtime as fallback). Tags / Status will be filled by the frontmatter pass
// added in chunk 3 — for now they're zero-valued, which matches the audit's
// observation that detail notes have no frontmatter in practice.
//
// MEMORY.md is skipped (it's the index, not a detail note). Errors from
// individual os.Stat calls drop that one entry rather than failing the
// whole walk — a note we couldn't stat is missing in a useful sense.
func (s *fsStore) detailEntries() ([]Entry, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "memory", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)

	entries := make([]Entry, 0, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		if name == "MEMORY.md" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		updated := info.ModTime()
		entries = append(entries, Entry{
			Name:    strings.TrimSuffix(name, filepath.Ext(name)),
			Path:    p,
			Title:   fileHook(p),
			Created: updated, // FS has no portable ctime; mtime is the right proxy
			Updated: updated,
		})
	}
	return entries, nil
}

// Recent returns detail notes whose mtime is within now-within, newest first.
func (s *fsStore) Recent(within time.Duration) ([]Entry, error) {
	all, err := s.detailEntries()
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-within)
	out := all[:0:len(all)]
	for _, e := range all {
		if e.Updated.After(cutoff) || e.Updated.Equal(cutoff) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}

// Stale returns detail notes whose mtime is older than now-olderThan, oldest
// first — candidates for review, promotion, or archive.
func (s *fsStore) Stale(olderThan time.Duration) ([]Entry, error) {
	all, err := s.detailEntries()
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-olderThan)
	out := all[:0:len(all)]
	for _, e := range all {
		if e.Updated.Before(cutoff) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.Before(out[j].Updated) })
	return out, nil
}
