package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// The regression this fixes: head-only truncation threw away the end of the
// output, which for a test run is the part that says what failed.
func TestClampKeepsTheVerdictAtTheEnd(t *testing.T) {
	var b strings.Builder
	b.WriteString("=== RUN   TestOne\n")
	for i := 0; i < 4000; i++ {
		fmt.Fprintf(&b, "--- PASS: TestFiller%04d (0.01s)\n", i)
	}
	b.WriteString("--- FAIL: TestPayments (0.20s)\n    payments/refund_test.go:73: want 5, got 4\nFAIL\n")
	full := b.String()
	if len(full) <= bashMaxOutput {
		t.Fatalf("test fixture is only %d bytes; it must exceed the cap", len(full))
	}

	got, elided := clampOutput(full)
	if elided == 0 {
		t.Fatal("expected the middle to be elided")
	}
	if !strings.Contains(got, "=== RUN   TestOne") {
		t.Error("the head should survive, so you can see what ran")
	}
	for _, want := range []string{"--- FAIL: TestPayments", "refund_test.go:73", "\nFAIL\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("the tail should survive; missing %q", want)
		}
	}
	if len(got) > bashMaxOutput+len(elisionMarker(elided))+2 {
		t.Errorf("clamped output is %d bytes, over the %d budget", len(got), bashMaxOutput)
	}
}

func TestClampLeavesShortOutputAlone(t *testing.T) {
	out := "just a few\nlines of output\n"
	got, elided := clampOutput(out)
	if got != out || elided != 0 {
		t.Errorf("short output should pass through untouched, got %q (elided %d)", got, elided)
	}
}

func TestClampCutsOnLineBoundaries(t *testing.T) {
	full := strings.Repeat("a line of predictable length here\n", 2000)
	got, _ := clampOutput(full)
	for _, ln := range strings.Split(got, "\n") {
		if ln == "" || strings.HasPrefix(ln, "...") {
			continue
		}
		if ln != "a line of predictable length here" {
			t.Fatalf("clamping split a line: %q", ln)
		}
	}
}

// The old code sliced bytes, which could cut a multi-byte rune in half.
func TestClampIsRuneSafe(t *testing.T) {
	// No newlines at all, so the line-boundary path can't help and the rune
	// fallback has to do the work.
	full := strings.Repeat("日本語のテキストです", 5000)
	got, elided := clampOutput(full)
	if elided == 0 {
		t.Fatal("expected truncation")
	}
	if !utf8.ValidString(got) {
		t.Error("clamping produced invalid UTF-8")
	}
	if strings.ContainsRune(got, '�') {
		t.Error("clamping produced replacement characters")
	}
}

func TestClampReportsHowMuchItRemoved(t *testing.T) {
	full := strings.Repeat("x\n", 40000)
	got, elided := clampOutput(full)
	if !strings.Contains(got, fmt.Sprintf("%d bytes elided", elided)) {
		t.Errorf("the notice should state the elided byte count, got:\n%s", oneLineOf(got))
	}
}

func TestPruneRemovesStaleSpillsOnly(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "bash-old.log")
	fresh := filepath.Join(dir, "bash-fresh.log")
	other := filepath.Join(dir, "keep-me.txt")
	for _, p := range []string{old, fresh, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stale := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, stale, stale); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, stale, stale); err != nil {
		t.Fatal(err)
	}

	pruneSpills(dir, spillMaxAge)

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("a stale spill should have been pruned")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh spill should be kept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("pruning must only touch its own .log files")
	}
}

// Spill files are named after the tool, and MCP tool names carry separators
// that CreateTemp rejects outright ("pattern contains path separator").
func TestSpillPrefixSanitisesToolNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"Grep", "Grep"},
		{"mcp__github__list_issues", "mcp__github__list_issues"},
		{"weird/name", "weird-name"},
		{"", "tool"},
	} {
		if got := spillPrefix(tc.name); got != tc.want {
			t.Errorf("spillPrefix(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSpillWritesUnderTheToolName(t *testing.T) {
	path, ok := SpillStore{Dir: t.TempDir()}.Spill("mcp__github__list_issues", "body")
	if !ok {
		t.Fatal("spill failed for an MCP-style tool name")
	}
	if !strings.Contains(filepath.Base(path), "mcp__github__list_issues") {
		t.Errorf("spill path %q should name the tool", path)
	}
}

// A store with no directory is what an embedder that chose none gets: the
// output is still clamped, but nothing is written anywhere and the notice
// promises no file.
func TestZeroSpillStoreClampsWithoutWriting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	var s SpillStore
	if path, ok := s.Spill("bash", "body"); ok || path != "" {
		t.Fatalf("Spill on the zero store = %q, %v; want nothing written", path, ok)
	}
	out, cut := s.Cap("Grep", strings.Repeat("match\n", 20000))
	if !cut {
		t.Fatal("oversized output should still be clamped")
	}
	if strings.Contains(out, spillMarker) {
		t.Errorf("no file was written, so the notice must not name one:\n%s", oneLineOf(out))
	}
	for _, d := range []string{os.Getenv("HOME"), os.Getenv("TMPDIR")} {
		if entries, _ := os.ReadDir(d); len(entries) > 0 {
			t.Errorf("zero store wrote into %s: %d entries", d, len(entries))
		}
	}
}

func oneLineOf(s string) string {
	if len(s) > 300 {
		s = s[:150] + " … " + s[len(s)-150:]
	}
	return s
}
