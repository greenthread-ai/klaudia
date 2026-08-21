package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/greenthread-ai/klaudia/internal/agent"
)

func mkResult(name, content string, isErr bool) agent.Event {
	return agent.Event{Type: "tool_result", ToolName: name, Content: content, IsError: isErr}
}

var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func visibleText(s string) string { return ansiSeq.ReplaceAllString(s, "") }

// The headline copy regression: a rendered code block used to copy as
// "    \tfmt.Println(...)" followed by ~40 spaces of background padding.
func TestRenderedCodeBlockCopiesAsSource(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	src := "Here you go:\n\n```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```\n"
	m.appendMarkdown(src)

	out := visibleText(m.transcript.String())
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasSuffix(ln, " ") {
			t.Errorf("line is padded to width, so it copies with trailing spaces: %q", ln)
		}
	}
	for _, want := range []string{"func main() {", "\tfmt.Println(\"hello\")", "}"} {
		if !strings.Contains(out, "\n"+want) {
			t.Errorf("code should copy as source; missing exact line %q in:\n%s", want, out)
		}
	}
}

func TestTrimLinePaddingKeepsEscapes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"abc   ", "abc"},
		{"abc\x1b[0m   ", "abc\x1b[0m"},
		{"abc   \x1b[0m", "abc\x1b[0m"},
		{"\x1b[31mabc\x1b[0m   ", "\x1b[31mabc\x1b[0m"},
		{"a \x1b[0m b   ", "a \x1b[0m b"},
		{"   ", ""},
		{"", ""},
		{"no padding", "no padding"},
	} {
		if got := trimLinePadding(tc.in); got != tc.want {
			t.Errorf("trimLinePadding(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTabsSurviveRendering(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.renderEvent(mkResult("Bash", "NAME\tSTATUS\tAGE\npod-a\tRunning\t2d", false))
	if !strings.Contains(m.transcript.String(), "\t") {
		t.Error("tabs must survive so tabular tool output keeps its columns")
	}
}

// s[:240] used to slice bytes, splitting multi-byte runes into mojibake.
func TestPreviewTruncationIsRuneSafe(t *testing.T) {
	body := strings.Repeat("a", 238) + strings.Repeat("日本語ですよ", 40)
	m := newTestModel()
	m.resize(80, 24)
	m.renderEvent(mkResult("Bash", body, false))
	if strings.ContainsRune(m.transcript.String(), '�') {
		t.Error("preview truncation split a multi-byte rune")
	}
}

func TestPreviewReportsWhatItHid(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.renderEvent(mkResult("Bash", strings.Repeat("output line\n", 200), false))
	out := visibleText(m.transcript.String())
	if !strings.Contains(out, "more lines") || !strings.Contains(out, "/last") {
		t.Errorf("a clipped preview should say how much it hid and how to see it:\n%s", out)
	}
}

func TestClipPreview(t *testing.T) {
	text := "l1\nl2\nl3\nl4\nl5"
	got, dropped := clipPreview(text, 3, 1000)
	if got != "l1\nl2\nl3" || dropped != 2 {
		t.Errorf("clipPreview lines = (%q,%d)", got, dropped)
	}
	if got, _ := clipPreview("short", 8, 480); got != "short" {
		t.Errorf("short input should pass through, got %q", got)
	}
	if got, _ := clipPreview(strings.Repeat("é", 100), 8, 10); len([]rune(got)) != 10 {
		t.Errorf("rune budget not respected: %d runes", len([]rune(got)))
	}
}

func TestExtractFencedBlocks(t *testing.T) {
	md := "intro\n\n```go\nA\n```\n\nmid\n\n~~~py\nB\nB2\n~~~\n\nend\n"
	got := extractFencedBlocks(md)
	if len(got) != 2 {
		t.Fatalf("got %d blocks, want 2", len(got))
	}
	if got[0].lang != "go" || got[0].body != "A" {
		t.Errorf("block 0 = %+v", got[0])
	}
	if got[1].lang != "py" || got[1].body != "B\nB2" {
		t.Errorf("block 1 = %+v", got[1])
	}
}

func TestCopyTargets(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.lastAssistantText = "Try this:\n\n```sh\nls -la\n```\n"
	m.lastResult = "raw\ttool\toutput"

	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "Try this:"},
		{[]string{"code"}, "ls -la"},
		{[]string{"out"}, "raw\ttool\toutput"},
	} {
		got, _, err := m.copyTarget(tc.args)
		if err != nil {
			t.Fatalf("copyTarget(%v): %v", tc.args, err)
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("copyTarget(%v) = %q, want it to contain %q", tc.args, got, tc.want)
		}
	}
	if _, _, err := m.copyTarget([]string{"nonsense"}); err == nil {
		t.Error("an unknown target should be an error, not a silent copy")
	}
}

// The escape must go through the render path, not straight to stdout, or it
// races the renderer mid-frame.
func TestCopyQueuesSequenceForTheNextFrame(t *testing.T) {
	m := newTestModel()
	m.resize(80, 24)
	m.lastAssistantText = "some answer"

	m.copyToClipboard(nil)
	if !strings.HasPrefix(m.pendingOSC, "\x1b]52;") {
		t.Fatalf("expected a queued OSC 52 sequence, got %q", m.pendingOSC)
	}
	if !strings.HasPrefix(m.View(), "\x1b]52;") {
		t.Error("the sequence should be emitted on the next frame")
	}
	if m.pendingOSC != "" {
		t.Error("the sequence must be emitted exactly once")
	}
	if strings.HasPrefix(m.View(), "\x1b]52;") {
		t.Error("the sequence should not repeat on subsequent frames")
	}
}
