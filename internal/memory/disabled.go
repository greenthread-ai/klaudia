package memory

import "time"

// Disabled returns a no-op Store. Reads return zero values; writes return
// ErrDisabled. Callers (TUI / CLI / tool) can swap this in for the headless
// path that used to pass a nil *memory.Store, dropping the nil-guard
// branches that used to litter every call site.
//
// SyncLinks() returns nil rather than ErrDisabled — the eager call at
// cli/root.go:672 runs at startup for its side effect and treats errors as
// best-effort; surfacing ErrDisabled there would print a spurious warning.
// The other writes do return ErrDisabled so genuinely missing the headless
// case surfaces.
func Disabled() Store {
	return disabledStore{}
}

// DisabledKnowledge mirrors Disabled() for the Knowledge surface, used in
// the same headless paths.
func DisabledKnowledge() Knowledge {
	return disabledKnowledge{}
}

type disabledStore struct{}

func (disabledStore) Path() string                          { return "" }
func (disabledStore) Index() (string, error)                { return "", nil }
func (disabledStore) Entries() ([]string, error)            { return nil, nil }
func (disabledStore) Add(string) error                      { return ErrDisabled }
func (disabledStore) Search(string) ([]string, error)       { return nil, nil }
func (disabledStore) FilePointers() []string                { return nil }
func (disabledStore) SyncLinks() error                      { return nil } // best-effort no-op; see Disabled docs
func (disabledStore) Recent(time.Duration) ([]Entry, error) { return nil, nil }
func (disabledStore) Stale(time.Duration) ([]Entry, error)  { return nil, nil }
func (disabledStore) ByTag(string) ([]Entry, error)         { return nil, nil }
func (disabledStore) Promote(string) error                  { return ErrDisabled }
func (disabledStore) Supersede(string, string) error        { return ErrDisabled }

type disabledKnowledge struct{}

func (disabledKnowledge) Path() string          { return "" }
func (disabledKnowledge) Read() (string, error) { return "", nil }
func (disabledKnowledge) Add(text string) error { return ErrDisabled }
