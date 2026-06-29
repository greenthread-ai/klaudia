package memory

import (
	"errors"
	"os"
	"path/filepath"
)

// Knowledge is the curated, durable project-knowledge surface backed by
// .klaudia/KNOWLEDGE.md. Separate from Store because its lifecycle is
// genuinely different — episodic memory is appended-to and ages out;
// knowledge is curated, near-canonical, and gets injected into the system
// prompt verbatim every session.
//
// A future pgmarkdown-backed implementation will satisfy this same
// interface against a "status: knowledge" partition of the documents table.
type Knowledge interface {
	Path() string
	Read() (string, error)
	Add(text string) error
}

// FSKnowledge is the filesystem-backed Knowledge implementation. Rooted at
// the same dir as fsStore (typically ".klaudia"), it reads and appends to
// KNOWLEDGE.md alongside MEMORY.md.
type FSKnowledge struct {
	dir string
}

// NewKnowledge returns the default filesystem-backed Knowledge rooted at
// dir. Mirrors New() for the Store interface so callers can construct both
// from the same .klaudia directory.
func NewKnowledge(dir string) Knowledge {
	return &FSKnowledge{dir: dir}
}

// Path returns the .klaudia/KNOWLEDGE.md path.
func (k *FSKnowledge) Path() string {
	return filepath.Join(k.dir, "KNOWLEDGE.md")
}

// Read returns the contents of KNOWLEDGE.md, or "" if it doesn't exist yet.
// Matches the no-file-no-error contract Index() uses on the Store side so the
// system-prompt assembler can stay branchless.
func (k *FSKnowledge) Read() (string, error) {
	data, err := os.ReadFile(k.Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// Add appends a timestamped bullet to KNOWLEDGE.md. Empty text returns
// ErrEmpty so callers can errors.Is check rather than parsing strings.
func (k *FSKnowledge) Add(text string) error {
	return appendBullet(k.Path(), "# Project Knowledge\n\n", text)
}

// Knowledge returns the curated-knowledge surface scoped to this store's
// directory. Lets Promote (and future callers) reach KNOWLEDGE.md without
// re-deriving cwd.
func (s *fsStore) Knowledge() Knowledge {
	return NewKnowledge(s.dir)
}
