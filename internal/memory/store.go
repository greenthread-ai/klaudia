package memory

// Store is klaudia's substitutable memory backend. The current filesystem
// implementation lives in fs.go (fsStore); a future Postgres implementation
// against ../pgmarkdown will land behind the same interface — see the
// conformance suite in conformance_test.go for the contract it must satisfy.
//
// Callers depend on this interface, never the concrete type. Headless callers
// can use Disabled() (added in a later chunk) instead of a nil interface
// value to drop nil-guard boilerplate.
//
// TODO(pgmarkdown): a pgStore satisfying this interface will land when
// ../pgmarkdown is buildable. The conformance suite is the contract.
type Store interface {
	// Path identifies the backend's location for /memory display. FS returns
	// a filesystem path; PG will return a logical identifier like
	// "pg:klaudia/MEMORY".
	Path() string

	// Index returns the always-loaded recall surface — MEMORY.md contents on
	// FS, a materialised view on PG. Empty string when nothing has been saved.
	Index() (string, error)

	// Entries returns the parsed session bullets from the index (the
	// timestamp + text pairs added via Add), one per slice element.
	Entries() ([]string, error)

	// Add appends a timestamped bullet to the index. Returns ErrEmpty when
	// text is whitespace-only.
	Add(text string) error

	// Search returns matching lines from the index plus the detail notes.
	// Detail-note hits are tagged "name.md: <line>" so callers know which
	// file to open. Empty query returns just the index entries.
	Search(query string) ([]string, error)

	// FilePointers returns Markdown link lines, one per detail note, used to
	// rebuild the "## Linked memory" section of the index.
	FilePointers() []string

	// SyncLinks refreshes the linked-memory section so it lists exactly the
	// current detail notes. Idempotent — only writes when the file would
	// change.
	SyncLinks() error
}

// New returns the default filesystem-backed Store rooted at dir (typically
// ".klaudia"). Returns the Store interface rather than the concrete type so
// callers don't depend on which backend they got.
func New(dir string) Store {
	return &fsStore{dir: dir}
}
