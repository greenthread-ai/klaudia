package memory

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// RunStoreSuite exercises every method on Store against a fresh backend
// produced by `factory`, asserting behavioural contracts that ANY backend
// must satisfy — not on-disk file shapes, those stay in fs_test.go and PG-
// specific tests will land alongside the PG impl.
//
// `wantWrites` toggles between the two contract shapes:
//   - true  → writes must succeed (or fail with their own semantic error
//     like ErrEmpty / ErrNotFound) and reads must observe them.
//   - false → writes must return ErrDisabled, reads return zero values,
//     observation is meaningless.
//
// When ../pgmarkdown ships, the same suite drives the PG implementation:
//
//	func TestPgStoreConformance(t *testing.T) {
//	    RunStoreSuite(t, "pg", func(t *testing.T) Store {
//	        return NewPg(testDSN(t))
//	    }, true)
//	}
//
// This is the contract document for the pgmarkdown PRD's Phase 7
// (filesystem ingestion) — a PG impl that passes RunStoreSuite is
// substitutable in klaudia today.
func RunStoreSuite(t *testing.T, name string, factory func(t *testing.T) Store, wantWrites bool) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Run("ReadEmptyIsZero", func(t *testing.T) { testReadEmptyIsZero(t, factory(t)) })
		t.Run("PathIsStable", func(t *testing.T) { testPathIsStable(t, factory(t)) })
		t.Run("SearchEmptyOnEmptyStore", func(t *testing.T) { testSearchEmptyOnEmptyStore(t, factory(t)) })
		t.Run("FilePointersOnEmpty", func(t *testing.T) { testFilePointersOnEmpty(t, factory(t)) })
		t.Run("SyncLinksOnEmptyIsNoOp", func(t *testing.T) { testSyncLinksOnEmpty(t, factory(t)) })
		t.Run("RecentEmpty", func(t *testing.T) { testRecentEmpty(t, factory(t)) })
		t.Run("StaleEmpty", func(t *testing.T) { testStaleEmpty(t, factory(t)) })
		t.Run("ByTagEmpty", func(t *testing.T) { testByTagEmpty(t, factory(t)) })

		if wantWrites {
			t.Run("AddRoundTrips", func(t *testing.T) { testAddRoundTrips(t, factory(t)) })
			t.Run("AddEmptyReturnsErrEmpty", func(t *testing.T) { testAddEmpty(t, factory(t)) })
			t.Run("PromoteMissingReturnsErrNotFound", func(t *testing.T) { testPromoteMissing(t, factory(t)) })
			t.Run("SupersedeMissingReturnsErrNotFound", func(t *testing.T) { testSupersedeMissing(t, factory(t)) })
		} else {
			t.Run("WritesReturnErrDisabled", func(t *testing.T) { testWritesReturnErrDisabled(t, factory(t)) })
		}
	})
}

// --- shared assertions (deliberately small + readable) ---

func testReadEmptyIsZero(t *testing.T, s Store) {
	idx, err := s.Index()
	if err != nil {
		t.Errorf("Index err = %v", err)
	}
	if idx != "" {
		t.Errorf("Index = %q, want empty on fresh store", idx)
	}
	entries, err := s.Entries()
	if err != nil {
		t.Errorf("Entries err = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Entries = %v, want empty on fresh store", entries)
	}
}

func testPathIsStable(t *testing.T, s Store) {
	// Two reads must return the same identifier — the value is opaque
	// (filesystem path on FS, doc id on PG, "" on Disabled) but it must
	// not change between calls.
	a, b := s.Path(), s.Path()
	if a != b {
		t.Errorf("Path() not stable: %q vs %q", a, b)
	}
}

func testSearchEmptyOnEmptyStore(t *testing.T, s Store) {
	got, err := s.Search("anything")
	if err != nil {
		t.Errorf("Search err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Search = %v, want empty on fresh store", got)
	}
}

func testFilePointersOnEmpty(t *testing.T, s Store) {
	if got := s.FilePointers(); len(got) != 0 {
		t.Errorf("FilePointers = %v, want empty on fresh store", got)
	}
}

func testSyncLinksOnEmpty(t *testing.T, s Store) {
	// Must not error even when nothing exists to link. This is the
	// startup-eager call from cli/root.go:672.
	if err := s.SyncLinks(); err != nil {
		t.Errorf("SyncLinks err = %v on fresh store, want nil", err)
	}
}

func testRecentEmpty(t *testing.T, s Store) {
	got, err := s.Recent(time.Hour)
	if err != nil || len(got) != 0 {
		t.Errorf("Recent = (%v, %v) on fresh store, want empty/nil", got, err)
	}
}

func testStaleEmpty(t *testing.T, s Store) {
	got, err := s.Stale(time.Hour)
	if err != nil || len(got) != 0 {
		t.Errorf("Stale = (%v, %v) on fresh store, want empty/nil", got, err)
	}
}

func testByTagEmpty(t *testing.T, s Store) {
	got, err := s.ByTag("tag")
	if err != nil || len(got) != 0 {
		t.Errorf("ByTag = (%v, %v) on fresh store, want empty/nil", got, err)
	}
}

func testAddRoundTrips(t *testing.T, s Store) {
	if err := s.Add("hello"); err != nil {
		t.Fatalf("Add err = %v", err)
	}
	idx, err := s.Index()
	if err != nil {
		t.Fatalf("Index err = %v", err)
	}
	if !strings.Contains(idx, "hello") {
		t.Errorf("Index after Add = %q, want it to contain %q", idx, "hello")
	}
	entries, _ := s.Entries()
	if len(entries) != 1 {
		t.Errorf("Entries after Add = %d, want 1", len(entries))
	}
}

func testAddEmpty(t *testing.T, s Store) {
	if err := s.Add("   \n  "); !errors.Is(err, ErrEmpty) {
		t.Errorf("Add(whitespace) err = %v, want ErrEmpty", err)
	}
}

func testPromoteMissing(t *testing.T, s Store) {
	if err := s.Promote("not-a-note"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Promote(missing) err = %v, want ErrNotFound", err)
	}
}

func testSupersedeMissing(t *testing.T, s Store) {
	if err := s.Supersede("ghost", "phantom"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Supersede(missing) err = %v, want ErrNotFound", err)
	}
}

func testWritesReturnErrDisabled(t *testing.T, s Store) {
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

// --- wire the suite into the existing FS + Disabled test packages ---

func TestFSStoreConformance(t *testing.T) {
	RunStoreSuite(t, "fs", func(t *testing.T) Store {
		return New(t.TempDir())
	}, true)
}

func TestDisabledStoreConformance(t *testing.T) {
	RunStoreSuite(t, "disabled", func(t *testing.T) Store {
		return Disabled()
	}, false)
}
