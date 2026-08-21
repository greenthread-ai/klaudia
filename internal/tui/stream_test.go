package tui

import (
	"strings"
	"testing"
)

// safePrefixOf feeds the whole text in one go and returns the flushable prefix.
func safePrefixOf(text string) string {
	var s streamScan
	s.advance([]byte(text))
	return text[:s.safe]
}

// safePrefixRuneByRune feeds one rune at a time. Incremental scanning must
// reach exactly the same answer as a single pass, or output would depend on how
// the network happened to chunk the stream.
func safePrefixRuneByRune(text string) string {
	var s streamScan
	var buf strings.Builder
	for _, r := range text {
		buf.WriteRune(r)
		s.advance([]byte(buf.String()))
	}
	return text[:s.safe]
}

func TestSafePrefixSplitsProse(t *testing.T) {
	text := "First paragraph.\n\nSecond paragraph.\n\nThird.\n"
	got := safePrefixOf(text)
	want := "First paragraph.\n\nSecond paragraph.\n\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The most important guarantee: never split inside a fenced code block.
func TestSafePrefixNeverSplitsFencedCode(t *testing.T) {
	text := "Intro line.\n\n```go\nfunc main() {\n\n\tx := 1\n\n\tprintln(x)\n}\n```\n\nAfter.\n"
	got := safePrefixOf(text)
	if strings.Contains(got, "```go") && !strings.Contains(got, "```\n\n") {
		t.Fatalf("prefix ends inside the code block:\n%q", got)
	}
	// The blank lines *inside* the fence must not have been treated as
	// boundaries, so the prefix can only be the intro or the whole thing.
	if got != "Intro line.\n\n" && !strings.HasSuffix(got, "```\n\n") {
		t.Fatalf("unexpected split point: %q", got)
	}
}

func TestSafePrefixHoldsOpenFence(t *testing.T) {
	// A fence that has not closed yet: nothing after it may be flushed.
	text := "Here:\n\n```go\nfunc main() {\n\n\tprintln(1)\n"
	got := safePrefixOf(text)
	if got != "Here:\n\n" {
		t.Fatalf("an unclosed fence must block flushing past it, got %q", got)
	}
}

func TestSafePrefixKeepsLooseListsIntact(t *testing.T) {
	// Blank lines inside a loose ordered list are structural. Splitting here
	// would restart the numbering — permanently, in scrollback.
	text := "1. first\n\n2. second\n\n3. third\n\nDone.\n"
	got := safePrefixOf(text)
	if strings.Contains(got, "1. first") && !strings.Contains(got, "3. third") {
		t.Fatalf("split inside a loose ordered list: %q", got)
	}
}

func TestSafePrefixKeepsBlockquotesIntact(t *testing.T) {
	text := "> line one\n\n> line two\n\nAfter.\n"
	got := safePrefixOf(text)
	if strings.Contains(got, "line one") && !strings.Contains(got, "line two") {
		t.Fatalf("split inside a blockquote: %q", got)
	}
}

func TestSafePrefixTreatsIndentedBlockAsContinuation(t *testing.T) {
	text := "- item\n\n    indented continuation\n\nAfter.\n"
	got := safePrefixOf(text)
	if strings.Contains(got, "- item") && !strings.Contains(got, "indented continuation") {
		t.Fatalf("split before an indented continuation: %q", got)
	}
}

func TestSafePrefixSplitsBetweenDifferentBlockKinds(t *testing.T) {
	text := "- a list item\n\nA prose paragraph.\n\nMore.\n"
	got := safePrefixOf(text)
	if !strings.HasPrefix(got, "- a list item\n\n") {
		t.Fatalf("a list followed by prose is a real boundary, got %q", got)
	}
}

func TestIncrementalScanMatchesOneShot(t *testing.T) {
	cases := []string{
		"First.\n\nSecond.\n\nThird.\n",
		"Intro.\n\n```go\nfunc f() {\n\n\treturn\n}\n```\n\nOutro.\n",
		"1. one\n\n2. two\n\nprose\n\nmore prose\n",
		"> quote\n\n> more\n\nplain\n\ntail\n",
		"~~~\nraw\n\nblock\n~~~\n\nafter\n",
		"# Heading\n\nBody text.\n\n## Another\n\nMore.\n",
		"| a | b |\n|---|---|\n| 1 | 2 |\n\nAfter table.\n\nEnd.\n",
	}
	for _, text := range cases {
		if one, inc := safePrefixOf(text), safePrefixRuneByRune(text); one != inc {
			t.Errorf("scan depends on chunking for %q:\n one-shot: %q\n  rune:    %q", text, one, inc)
		}
	}
}

func TestScanRebaseAfterTrim(t *testing.T) {
	text := "First.\n\nSecond.\n\nThird.\n"
	var s streamScan
	s.advance([]byte(text))
	n := s.safe
	if n == 0 {
		t.Fatal("expected a flushable prefix")
	}
	rest := text[n:]
	s.rebase(n)
	if s.safe != 0 {
		t.Errorf("safe offset should rebase to 0, got %d", s.safe)
	}
	if s.scanned != len(rest) {
		t.Errorf("scanned = %d, want %d after rebase", s.scanned, len(rest))
	}
	// Continuing the scan on the trimmed buffer must still work.
	s.advance([]byte(rest + "\nFourth.\n\nFifth.\n"))
	if s.safe <= 0 {
		t.Error("scanner should keep finding boundaries after a rebase")
	}
}

func TestFenceToken(t *testing.T) {
	for _, tc := range []struct {
		line    string
		token   string
		closing bool
	}{
		{"```go", "```", false},
		{"```", "```", true},
		{"~~~~", "~~~~", true},
		{"  ```", "```", true},
		{"     ```", "", false}, // 5 spaces: indented code, not a fence
		{"``", "", false},
		{"plain text", "", false},
	} {
		tok, closing := fenceToken(tc.line)
		if tok != tc.token || (tok != "" && closing != tc.closing) {
			t.Errorf("fenceToken(%q) = (%q,%v), want (%q,%v)", tc.line, tok, closing, tc.token, tc.closing)
		}
	}
}

func TestClassifyLine(t *testing.T) {
	for _, tc := range []struct {
		line string
		want blockKind
	}{
		{"- bullet", kindList},
		{"* bullet", kindList},
		{"+ bullet", kindList},
		{"12. ordered", kindList},
		{"3) ordered", kindList},
		{"> quote", kindQuote},
		{"    indented", kindIndented},
		{"\ttabbed", kindIndented},
		{"plain prose", kindOther},
		{"3.14 is pi", kindOther}, // not a list: no space after the dot
	} {
		if got := classifyLine(tc.line); got != tc.want {
			t.Errorf("classifyLine(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// Short messages must not be chunked at all — they render in one glamour pass.
func TestShortStreamIsNotChunked(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.out.drainText()

	m.appendText("A short answer.\n\nWith two paragraphs.\n")
	if m.out.pending() != 0 {
		t.Fatal("a short message should not be committed until it completes")
	}
	m.flushAssistant()
	if m.out.pending() != 1 {
		t.Fatalf("expected exactly one committed block, got %d", m.out.pending())
	}
}

func TestLongStreamFlushesProgressively(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.out.drainText()

	for i := 0; i < 20; i++ {
		m.appendText("Paragraph number " + strings.Repeat("x", 5) + ".\n\n")
	}
	if m.out.pending() == 0 {
		t.Fatal("a long message should commit chunks while still streaming")
	}
	// Whatever is left unflushed is what the live region previews.
	if m.streamBuf.Len() == 0 {
		t.Error("the in-progress remainder should stay in the buffer")
	}
	m.flushAssistant()
	if m.streamBuf.Len() != 0 {
		t.Error("flushAssistant should drain the buffer")
	}
}

func TestStreamTailIsBounded(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	for i := 0; i < 50; i++ {
		m.appendText("line of streaming text\n")
	}
	tail := m.streamTail()
	if n := len(strings.Split(tail, "\n")); n > streamTailLines {
		t.Errorf("live preview is %d lines, want at most %d", n, streamTailLines)
	}
}
