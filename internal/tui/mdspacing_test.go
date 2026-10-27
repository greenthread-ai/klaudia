package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/greenthread-ai/klaudia/pkg/agent"
)

// A streamed reply is committed in chunks, each rendered on its own. glamour
// pads blank lines with spaces, so trimming "\n" kept them, and every code
// block or list at a chunk boundary sat between two blank lines. And tables
// were stretched to the full width, a short second column far from the first.
func TestStreamedMarkdownSpacingAndTables(t *testing.T) {
	m := newTestModel()
	m.resize(100, 30)
	text := "Renamed it.\n\n| File | Change |\n|---|---|\n| `main.go:26` | Call site |\n\nI left `counts` alone.\n\n```go\nfunc tally(text string) map[string]int\n```\n\n- one\n- two\n\nDone."
	for _, part := range strings.SplitAfter(text, " ") {
		m.renderEvent(agent.Event{Type: "assistant", Text: part})
	}
	m.flushAssistant()
	out := stripANSI(m.transcript.String())

	lines := strings.Split(out, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" && strings.TrimSpace(lines[i-1]) == "" {
			t.Errorf("two blank lines in a row at line %d:\n%s", i, out)
			break
		}
	}
	for _, ln := range lines {
		if strings.Contains(ln, "Call site") && len([]rune(strings.TrimRight(ln, " "))) > 40 {
			t.Errorf("the table is stretched: %q", ln)
		}
	}
}

// A | inside a fenced block is code, not a table.
func TestSplitTablesLeavesCodeAlone(t *testing.T) {
	segs := splitTables("```\n| a | b |\n|---|---|\n```\n\n| x | y |\n|---|---|\n| 1 | 2 |")
	tables := 0
	for _, s := range segs {
		if s.table > 0 {
			tables++
			if !strings.Contains(s.text, "| 1 | 2 |") {
				t.Errorf("wrong table segment: %q", s.text)
			}
		}
	}
	if tables != 1 {
		t.Errorf("found %d tables, want 1: %+v", tables, segs)
	}
}

// Inline code copies as itself: no padding spaces in colour, and backticks
// rather than bare words without it.
func TestInlineCodeIsCopySafe(t *testing.T) {
	m := newTestModel() // no colour in tests
	m.resize(100, 30)
	if got := strings.TrimSpace(stripANSI(m.markdown("Run `go test` now."))); got != "Run `go test` now." {
		t.Errorf("no colour: %q", got)
	}
	defer lipgloss.SetColorProfile(lipgloss.ColorProfile())
	lipgloss.SetColorProfile(termenv.TrueColor)
	m.buildGlamour(100)
	if got := strings.TrimSpace(stripANSI(m.markdown("Run `go test` now."))); got != "Run go test now." {
		t.Errorf("colour: %q", got)
	}
}
