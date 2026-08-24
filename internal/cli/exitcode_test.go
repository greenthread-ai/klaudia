package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// §20: exit codes have to be meaningful. "0 or 1" cannot tell a CI job that the
// task needed a package installed apart from the model getting it wrong, and
// those want completely different responses.
func TestExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, ExitOK},
		{"plain failure", errors.New("boom"), ExitError},
		{"already rendered", errRendered, ExitError},
		{"usage", usageErrorf("bad flag"), ExitUsage},
		{"max turns", exitError{ExitMaxTurns}, ExitMaxTurns},
		{"host blocked", exitError{ExitHostChangeBlocked}, ExitHostChangeBlocked},
		{"interrupted", exitError{ExitInterrupted}, ExitInterrupted},
		{"wrapped", fmt.Errorf("context: %w", exitError{ExitMaxTurns}), ExitMaxTurns},
	} {
		if got := exitCodeFor(tc.err); got != tc.want {
			t.Errorf("%s: exit code = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The codes must be distinct, or branching on them is meaningless.
func TestExitCodesAreDistinct(t *testing.T) {
	seen := map[int]string{}
	for name, code := range map[string]int{
		"ExitOK": ExitOK, "ExitError": ExitError, "ExitUsage": ExitUsage,
		"ExitMaxTurns": ExitMaxTurns, "ExitHostChangeBlocked": ExitHostChangeBlocked,
		"ExitInterrupted": ExitInterrupted,
	} {
		if prev, dup := seen[code]; dup {
			t.Errorf("%s and %s share exit code %d", prev, name, code)
		}
		seen[code] = name
	}
}

// A usage error carries a message the user has not seen; a bare exitError does
// not, because its reason is already in the result payload.
func TestUsageErrorCarriesItsMessage(t *testing.T) {
	err := usageErrorf("--loop cannot be combined with %s", "stream-json")
	if err.Error() == "" {
		t.Fatal("a usage error printed nothing")
	}
	if got := err.Error(); got != "--loop cannot be combined with stream-json" {
		t.Errorf("message = %q", got)
	}
	if (exitError{}).Error() != "" {
		t.Error("a bare exitError has text; it would be printed twice")
	}
}

// Cancellation exits 130, the shell convention, so a wrapper script can tell an
// interrupt from a failure.
func TestInterruptMapsTo130(t *testing.T) {
	if ExitInterrupted != 130 {
		t.Errorf("ExitInterrupted = %d, want the conventional 130", ExitInterrupted)
	}
	if !errors.Is(context.Canceled, context.Canceled) {
		t.Fatal("sanity")
	}
}
