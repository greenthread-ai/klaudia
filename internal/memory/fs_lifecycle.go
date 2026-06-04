package memory

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Promote moves a detail note's content into KNOWLEDGE.md and marks the
// source superseded. The source file is rewritten with status:superseded
// and superseded_by:KNOWLEDGE.md rather than deleted — this keeps the audit
// trail walkable (you can still find the original note via Stale or a
// future history lookup), matches pgmarkdown's supersession-graph model,
// and gives a hypothetical "unpromote" path a fighting chance.
//
// Body is the frontmatter-stripped content, trimmed of leading/trailing
// whitespace. An empty body returns ErrEmpty rather than silently appending
// nothing to KNOWLEDGE.md.
func (s *fsStore) Promote(name string) error {
	notePath := filepath.Join(s.dir, "memory", name+".md")
	data, err := os.ReadFile(notePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	meta, body := parseFrontmatter(data)

	bodyText := strings.TrimSpace(string(body))
	if bodyText == "" {
		return ErrEmpty
	}
	if err := s.Knowledge().Add(bodyText); err != nil {
		return err
	}

	meta.Status = "superseded"
	meta.SupersededBy = "KNOWLEDGE.md"
	if meta.Updated.IsZero() {
		meta.Updated = time.Now()
	}
	return writeFrontmatter(notePath, meta, body)
}

// Supersede records that oldName has been replaced by newName. Both files
// must exist. Rewrites the old note's frontmatter (status: superseded,
// superseded_by: newName) and the new note's frontmatter (supersedes:
// oldName). Idempotent — calling twice produces the same output.
func (s *fsStore) Supersede(oldName, newName string) error {
	oldPath := filepath.Join(s.dir, "memory", oldName+".md")
	newPath := filepath.Join(s.dir, "memory", newName+".md")

	oldData, err := os.ReadFile(oldPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	newData, err := os.ReadFile(newPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}

	oldMeta, oldBody := parseFrontmatter(oldData)
	newMeta, newBody := parseFrontmatter(newData)

	// Idempotent: if already linked in both directions, no further work.
	if oldMeta.SupersededBy == newName && oldMeta.Status == "superseded" && newMeta.Supersedes == oldName {
		return nil
	}

	oldMeta.Status = "superseded"
	oldMeta.SupersededBy = newName
	newMeta.Supersedes = oldName

	if err := writeFrontmatter(oldPath, oldMeta, oldBody); err != nil {
		return err
	}
	return writeFrontmatter(newPath, newMeta, newBody)
}

// writeFrontmatter rewrites path with the given frontmatter + body. If meta
// has no non-zero fields, the file is written body-only (no fence) — that
// preserves the no-frontmatter shape for notes that never had one. Used by
// Promote / Supersede; safe to call repeatedly.
func writeFrontmatter(path string, meta frontmatter, body []byte) error {
	var buf bytes.Buffer
	if isFrontmatterNonZero(meta) {
		b, err := yaml.Marshal(meta)
		if err != nil {
			return err
		}
		buf.Write(fmFence)
		buf.Write(b)
		buf.Write(fmFence)
		buf.WriteByte('\n')
	}
	buf.Write(body)
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// isFrontmatterNonZero reports whether any tracked field carries data —
// the guard for whether to emit the YAML fence at all.
func isFrontmatterNonZero(m frontmatter) bool {
	return len(m.Tags) > 0 ||
		!m.Created.IsZero() ||
		!m.Updated.IsZero() ||
		m.Status != "" ||
		m.Supersedes != "" ||
		m.SupersededBy != ""
}
