package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/greenthread-ai/klaudia/internal/sandbox"
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

func TestSpillWritesFullOutputAndFormatNamesIt(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())

	full := strings.Repeat("build output line\n", 5000)
	out, fullOut := formatBashOutput(sandbox.Response{Stdout: full}, "make build")

	if !strings.Contains(out, "full output:") {
		t.Fatalf("truncated output should name the spill file:\n%s", oneLineOf(out))
	}
	path := spillPathFrom(out)
	if path == "" {
		t.Fatal("could not parse the spill path out of the notice")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable: %v", err)
	}
	if string(data) != full {
		t.Errorf("spill should hold the complete output: got %d bytes, want %d", len(data), len(full))
	}
	// The UI channel carries the same untruncated text, without a disk read.
	if fullOut != full {
		t.Errorf("Full = %d bytes, want the untruncated %d", len(fullOut), len(full))
	}
	if len(out) >= len(fullOut) {
		t.Error("the model-facing copy should be shorter than the full one")
	}
}

func TestNoSpillForShortOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KLAUDIA_CONFIG_DIR", dir)

	out, _ := formatBashOutput(sandbox.Response{Stdout: "all good\n"}, "echo hi")
	if strings.Contains(out, "full output:") {
		t.Error("short output needs no spill file")
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "outputs")); err == nil && len(entries) > 0 {
		t.Errorf("no spill file should have been written, found %d", len(entries))
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
		t.Error("pruning must only touch its own bash-*.log files")
	}
}

// Exit status and timeout annotations must still land after clamping.
func TestExitAnnotationSurvivesClamping(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	out, fullOut := formatBashOutput(sandbox.Response{
		Stdout: strings.Repeat("noise\n", 20000), ExitCode: 1,
	}, "go test ./...")
	if !strings.Contains(out, "[exit code 1]") {
		t.Errorf("exit annotation lost:\n%s", oneLineOf(out))
	}
	// A reader of the full output still needs to know the command failed.
	if !strings.Contains(fullOut, "[exit code 1]") {
		t.Error("exit annotation missing from the full output")
	}
}

func spillPathFrom(out string) string {
	const marker = "[full output: "
	i := strings.Index(out, marker)
	if i < 0 {
		return ""
	}
	rest := out[i+len(marker):]
	j := strings.IndexByte(rest, ']')
	if j < 0 {
		return ""
	}
	return rest[:j]
}

func oneLineOf(s string) string {
	if len(s) > 300 {
		s = s[:150] + " … " + s[len(s)-150:]
	}
	return s
}

// End to end through the real Bash tool with a real command.
func TestBashEndToEndKeepsHeadAndTail(t *testing.T) {
	t.Setenv("KLAUDIA_CONFIG_DIR", t.TempDir())
	b, err := NewBash(sandbox.NewLocal())
	if err != nil {
		t.Fatal(err)
	}
	res, err := b.Execute(context.Background(), Context{}, []byte(`{"command":"echo FIRST_LINE_MARKER; for i in $(seq 1 6000); do echo filler line $i padding padding padding; done; echo LAST_LINE_MARKER; exit 3"}`))
	if err != nil {
		t.Fatal(err)
	}
	out := res[0].Content
	t.Logf("model sees %d bytes, UI gets %d", len(out), len(res[0].Display()))
	if res[0].Full == "" {
		t.Error("a clamped result must carry the untruncated text for the UI")
	}
	if len(res[0].Display()) <= len(out) {
		t.Error("Display() should be the longer, untruncated variant")
	}
	for _, want := range []string{"FIRST_LINE_MARKER", "LAST_LINE_MARKER", "bytes elided", "[exit code 3]", "full output:"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if path := spillPathFrom(out); path == "" {
		t.Fatal("the model-facing output should name the spill file it can read")
	}
}
