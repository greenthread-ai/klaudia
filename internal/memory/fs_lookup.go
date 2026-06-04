package memory

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// detailEntries walks .klaudia/memory/*.md once and returns one Entry per
// note. Fields populated today:
//
//   - Name, Path, Title — always.
//   - Updated, Created — file mtime is the default. Frontmatter `updated`
//     or `created` values override when present.
//   - Tags, Status — from frontmatter when present, zero values otherwise.
//
// MEMORY.md is skipped (it's the index, not a detail note). Errors from
// individual os.Stat / os.ReadFile calls drop that one entry rather than
// failing the whole walk — a note we couldn't read is missing in a useful
// sense.
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
		e := Entry{
			Name:    strings.TrimSuffix(name, filepath.Ext(name)),
			Path:    p,
			Title:   fileHook(p),
			Created: updated, // FS has no portable ctime; mtime is the right proxy
			Updated: updated,
		}
		// Frontmatter overrides mtime where present; absent values leave
		// the mtime-based defaults in place. Read failures fall through to
		// the no-frontmatter shape rather than dropping the entry.
		if raw, rerr := os.ReadFile(p); rerr == nil {
			meta, _ := parseFrontmatter(raw)
			if !meta.Updated.IsZero() {
				e.Updated = meta.Updated
			}
			if !meta.Created.IsZero() {
				e.Created = meta.Created
			}
			e.Tags = meta.Tags
			e.Status = meta.Status
		}
		entries = append(entries, e)
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

// ByTag returns detail notes whose frontmatter `tags` list contains tag.
// Files without frontmatter contribute nothing — matching the audit's
// observation that today's notes have no tags. As notes acquire
// frontmatter (manually authored or via Promote / Supersede rewrites),
// they become discoverable here without any change to existing data.
func (s *fsStore) ByTag(tag string) ([]Entry, error) {
	all, err := s.detailEntries()
	if err != nil {
		return nil, err
	}
	out := all[:0:len(all)]
	for _, e := range all {
		if slices.Contains(e.Tags, tag) {
			out = append(out, e)
		}
	}
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
