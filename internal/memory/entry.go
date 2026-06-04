package memory

import "time"

// Entry is the structured view of one detail note used by the lookup-mode
// methods (Recent / Stale / ByTag). The current FS implementation fills
// Name / Path / Title / Updated from the filesystem; Created falls back to
// Updated when ctime isn't reliably available on the platform. Tags and
// Status come from YAML frontmatter when present and stay nil / empty for
// the no-frontmatter notes that dominate today's stores — that's not a
// failure mode, just "no metadata to filter on".
//
// A future Postgres backend (../pgmarkdown) fills every field from the row
// directly; the conformance suite asserts the same shape for both.
type Entry struct {
	// Name is the basename of the note without extension ("tools" for
	// memory/tools.md). It's what callers pass to Promote / Supersede.
	Name string

	// Path is the backend-specific identifier — a filesystem path on FS,
	// a logical doc id on PG. /memory and other UI paths display it verbatim.
	Path string

	// Title is a one-line hook (the first heading with leading "#" stripped,
	// or the first non-empty line, capped to 80 chars). Sourced via fileHook
	// on the FS impl so it tracks whatever convention the note happens to use.
	Title string

	// Tags is the frontmatter `tags:` list when present, nil otherwise.
	// ByTag matches against this; notes without frontmatter never match.
	Tags []string

	// Created and Updated are the note's timestamps. FS falls back to file
	// mtime for both when frontmatter values aren't present; PG reads them
	// from the parsed row.
	Created time.Time
	Updated time.Time

	// Status is the lifecycle marker — typically "" (no frontmatter),
	// "active", or "superseded". The Promote / Supersede ops rewrite it.
	Status string
}
