package session

import (
	"os"
	"strings"
	"testing"
)

func TestSummaryRoundTrip(t *testing.T) {
	cwd := t.TempDir()
	if err := WriteSummary(cwd, "sess-1", "The user refactored the API client and all tests pass.", "abc1234"); err != nil {
		t.Fatal(err)
	}
	body, ok := ReadSummary(cwd, "sess-1")
	if !ok {
		t.Fatal("ReadSummary returned !ok")
	}
	if body != "The user refactored the API client and all tests pass." {
		t.Errorf("body = %q (header not stripped?)", body)
	}
	// The on-disk file keeps the stamped header.
	raw, _ := os.ReadFile(SummaryPath(cwd, "sess-1"))
	if !strings.Contains(string(raw), "# Session summary sess-1") || !strings.Contains(string(raw), "abc1234") {
		t.Errorf("raw missing header/commit: %q", raw)
	}
}

func TestReadSummaryMissing(t *testing.T) {
	if _, ok := ReadSummary(t.TempDir(), "nope"); ok {
		t.Error("expected !ok for missing summary")
	}
}
