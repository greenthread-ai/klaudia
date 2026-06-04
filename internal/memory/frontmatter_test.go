package memory

import (
	"strings"
	"testing"
	"time"
)

func TestParseFrontmatterNoneReturnsZeroAndContentUnchanged(t *testing.T) {
	// The common case today: detail notes have no frontmatter. The body
	// must come back unchanged so the existing fileHook / Search code keeps
	// working byte-identically on every note that's been written so far.
	content := []byte("# Just a heading\n\nbody text\n")
	meta, body := parseFrontmatter(content)
	if !meta.Updated.IsZero() || meta.Status != "" || meta.Tags != nil {
		t.Errorf("no-frontmatter case must yield zero meta; got %+v", meta)
	}
	if string(body) != string(content) {
		t.Errorf("body should be unchanged when no frontmatter present; got %q", body)
	}
}

func TestParseFrontmatterExtractsKnownFields(t *testing.T) {
	content := []byte("---\n" +
		"tags: [memory, cli]\n" +
		"status: active\n" +
		"created: 2026-01-01T00:00:00Z\n" +
		"updated: 2026-06-01T12:00:00Z\n" +
		"superseded_by: old-decision\n" +
		"---\n\n" +
		"# Title\n\nbody here\n")
	meta, body := parseFrontmatter(content)
	if len(meta.Tags) != 2 || meta.Tags[0] != "memory" || meta.Tags[1] != "cli" {
		t.Errorf("tags = %v, want [memory cli]", meta.Tags)
	}
	if meta.Status != "active" {
		t.Errorf("status = %q, want active", meta.Status)
	}
	if !meta.Updated.Equal(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("updated = %v, want 2026-06-01T12:00:00Z", meta.Updated)
	}
	if meta.SupersededBy != "old-decision" {
		t.Errorf("superseded_by = %q, want old-decision", meta.SupersededBy)
	}
	if !strings.HasPrefix(string(body), "# Title") {
		t.Errorf("body must start with the post-fence heading; got %q", body)
	}
}

func TestParseFrontmatterUnclosedFenceFallsBack(t *testing.T) {
	// A leading "---\n" with no closing fence is malformed. Rather than
	// erroring out (which would drop the note from the lookup walk), we
	// treat it as no-frontmatter and surface the whole thing as body.
	content := []byte("---\nthis was supposed to be frontmatter\n# heading\nbody\n")
	meta, body := parseFrontmatter(content)
	if !meta.Updated.IsZero() || meta.Status != "" || meta.Tags != nil {
		t.Errorf("unclosed-fence case must yield zero meta; got %+v", meta)
	}
	if string(body) != string(content) {
		t.Errorf("unclosed-fence body should be the input unchanged; got %q", body)
	}
}

func TestParseFrontmatterMalformedYamlFallsBack(t *testing.T) {
	// Defensive: a parse error on the YAML body shouldn't break the lookup
	// walk either. Title / mtime still work via the rest of the pipeline.
	content := []byte("---\ntags: [unclosed bracket\n---\n\nbody\n")
	meta, body := parseFrontmatter(content)
	if meta.Tags != nil {
		t.Errorf("malformed YAML must yield zero meta; got %+v", meta)
	}
	// Body falls back to the whole input — same shape as the unclosed-fence
	// case. The caller's title/search code keeps working.
	if string(body) != string(content) {
		t.Errorf("malformed-yaml body should be the input unchanged; got %q", body)
	}
}

func TestByTagMatchesFrontmatterTags(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeNote(t, dir, "tagged-cli.md",
		"---\ntags: [cli, ux]\n---\n\n# CLI thing\nbody\n", now)
	writeNote(t, dir, "tagged-memory.md",
		"---\ntags: [memory]\n---\n\n# Memory thing\nbody\n", now)
	writeNote(t, dir, "no-frontmatter.md", "# Plain\nbody\n", now)

	store := New(dir)
	got, err := store.ByTag("cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "tagged-cli" {
		t.Errorf("ByTag(cli) = %+v, want [tagged-cli]", got)
	}

	// A tag no note carries returns empty (not error). Notes without
	// frontmatter contribute nothing — that's the no-tag-no-match contract.
	if got, _ := store.ByTag("nope"); len(got) != 0 {
		t.Errorf("ByTag(nope) = %+v, want empty", got)
	}
}

func TestFrontmatterUpdatedOverridesMtime(t *testing.T) {
	// If the frontmatter declares an `updated:` value, that wins over the
	// filesystem mtime — important so an author can pin a note's
	// "last meaningful change" date independently of touch/git checkout.
	dir := t.TempDir()
	mtime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	declared := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	writeNote(t, dir, "decided.md",
		"---\nupdated: 2026-06-01T00:00:00Z\n---\n\n# Decision\n", mtime)

	store := New(dir)
	got, _ := store.Recent(365 * 24 * time.Hour)
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if !got[0].Updated.Equal(declared) {
		t.Errorf("Updated = %v, want frontmatter override %v", got[0].Updated, declared)
	}
}
