package memory

import (
	"errors"
	"testing"
	"time"
)

// TestDisabledReadsReturnZero locks the contract that callers can call any
// read method on a Disabled() Store without nil-checking and without
// errors — the read path is intentionally branchless so call sites stay
// clean. Any write attempt returns ErrDisabled instead.
func TestDisabledReadsReturnZero(t *testing.T) {
	s := Disabled()

	if got := s.Path(); got != "" {
		t.Errorf("Path() = %q, want empty", got)
	}
	if got, err := s.Index(); err != nil || got != "" {
		t.Errorf("Index() = (%q, %v), want empty/nil", got, err)
	}
	if got, err := s.Entries(); err != nil || len(got) != 0 {
		t.Errorf("Entries() = (%v, %v), want empty/nil", got, err)
	}
	if got, err := s.Search("anything"); err != nil || len(got) != 0 {
		t.Errorf("Search() = (%v, %v), want empty/nil", got, err)
	}
	if got := s.FilePointers(); got != nil {
		t.Errorf("FilePointers() = %v, want nil", got)
	}
	if got, err := s.Recent(time.Hour); err != nil || len(got) != 0 {
		t.Errorf("Recent() = (%v, %v), want empty/nil", got, err)
	}
	if got, err := s.Stale(time.Hour); err != nil || len(got) != 0 {
		t.Errorf("Stale() = (%v, %v), want empty/nil", got, err)
	}
	if got, err := s.ByTag("any"); err != nil || len(got) != 0 {
		t.Errorf("ByTag() = (%v, %v), want empty/nil", got, err)
	}
}

func TestDisabledWritesReturnErrDisabled(t *testing.T) {
	s := Disabled()
	if err := s.Add("x"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Add err = %v, want ErrDisabled", err)
	}
	if err := s.Promote("x"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Promote err = %v, want ErrDisabled", err)
	}
	if err := s.Supersede("a", "b"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Supersede err = %v, want ErrDisabled", err)
	}
}

func TestDisabledSyncLinksIsBestEffortNoOp(t *testing.T) {
	// SyncLinks is called eagerly at startup (cli/root.go:672) and treats
	// errors as best-effort. Returning ErrDisabled there would print a
	// spurious warning every headless run — return nil instead.
	if err := Disabled().SyncLinks(); err != nil {
		t.Errorf("SyncLinks should be a no-op on Disabled; got %v", err)
	}
}

func TestDisabledKnowledgeIsBranchless(t *testing.T) {
	k := DisabledKnowledge()
	if got, err := k.Read(); err != nil || got != "" {
		t.Errorf("Read() = (%q, %v), want empty/nil", got, err)
	}
	if got := k.Path(); got != "" {
		t.Errorf("Path() = %q, want empty", got)
	}
	if err := k.Add("x"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Add err = %v, want ErrDisabled", err)
	}
}
