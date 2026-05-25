package tui

import (
	"slices"
	"testing"
)

func TestTaglines(t *testing.T) {
	if len(taglines) != 32 {
		t.Errorf("want 32 taglines, got %d", len(taglines))
	}
	seen := map[string]bool{}
	for _, tl := range taglines {
		if tl == "" {
			t.Error("empty tagline")
		}
		if seen[tl] {
			t.Errorf("duplicate tagline: %q", tl)
		}
		seen[tl] = true
	}
	// randomTagline always returns a member of the set.
	for range 50 {
		if !slices.Contains(taglines, randomTagline()) {
			t.Fatal("randomTagline returned a value not in the set")
		}
	}
}
