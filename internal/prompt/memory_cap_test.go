package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCapMemoryIndex(t *testing.T) {
	small := "# Memory\n\n- one\n- two"
	if got := capMemoryIndex(small, 200, 25_000); got != small {
		t.Errorf("an index within budget changed: %q", got)
	}

	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, fmt.Sprintf("- note %d", i))
	}
	index := strings.Join(lines, "\n")

	got := capMemoryIndex(index, 4, 25_000)
	if !strings.HasPrefix(got, "- note 1\n- note 2\n- note 3\n- note 4\n\n(6 more lines — open .klaudia/MEMORY.md") {
		t.Errorf("line cap: %q", got)
	}
	if strings.Contains(got, "note 5") {
		t.Errorf("line cap kept a line past the budget: %q", got)
	}

	// The byte cap cuts at a line boundary too: "- note N\n" is 9 bytes.
	got = capMemoryIndex(index, 200, 20)
	if !strings.HasPrefix(got, "- note 1\n- note 2\n\n(8 more lines") {
		t.Errorf("byte cap: %q", got)
	}
}

func TestSystemCapsRecalledMemory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	klaudiaDir := filepath.Join(dir, ".klaudia")
	if err := os.MkdirAll(klaudiaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("# Memory\n\n")
	for i := 1; i <= 500; i++ {
		fmt.Fprintf(&b, "- 2026-09-29T10:00:00Z note number %d\n", i)
	}
	if err := os.WriteFile(filepath.Join(klaudiaDir, "MEMORY.md"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	p := System(dir, "")
	if !strings.Contains(p, "note number 1\n") || strings.Contains(p, "note number 500") {
		t.Error("recalled memory should keep the head of the index and drop the rest")
	}
	if !strings.Contains(p, "more lines — open .klaudia/MEMORY.md") {
		t.Error("a capped index should say how much was left out and where it is")
	}
}
